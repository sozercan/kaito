// Copyright (c) KAITO authors.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package scheduling

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/sku"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// GPUInfo represents detailed GPU information detected from nodes
type GPUInfo struct {
	GPUCount        int
	GPUMemoryGiB    float64
	GPUModel        string
	GPUDriver       string
	DetectionMethod string
	NodeName        string
}

// GPUDetector provides methods to detect GPU information from Kubernetes nodes
type GPUDetector interface {
	// DetectGPUInfo detects GPU information from cluster nodes
	DetectGPUInfo(ctx context.Context, nodeNames []string) ([]*GPUInfo, error)
	
	// GetAvailableGPUCapacity returns the available GPU capacity across nodes
	GetAvailableGPUCapacity(ctx context.Context, nodeNames []string) (*GPUCapacity, error)
}

// GPUCapacity represents the total GPU capacity available
type GPUCapacity struct {
	TotalGPUCount       int
	TotalGPUMemoryGiB   float64
	PerGPUMemoryGiB     float64
	GPUNodes            map[string]*GPUInfo
	HighestGPUMemoryGiB float64
}

// nodeGPUDetector implements GPUDetector using Kubernetes node information
type nodeGPUDetector struct {
	kubeClient client.Client
}

// NewGPUDetector creates a new GPU detector instance
func NewGPUDetector(kubeClient client.Client) GPUDetector {
	return &nodeGPUDetector{
		kubeClient: kubeClient,
	}
}

// DetectGPUInfo detects GPU information from cluster nodes
func (d *nodeGPUDetector) DetectGPUInfo(ctx context.Context, nodeNames []string) ([]*GPUInfo, error) {
	var gpuInfos []*GPUInfo
	
	for _, nodeName := range nodeNames {
		node, err := d.getNode(ctx, nodeName)
		if err != nil {
			klog.Warningf("Failed to get node %s: %v", nodeName, err)
			continue
		}
		
		gpuInfo := d.detectGPUFromNode(node)
		if gpuInfo != nil {
			gpuInfos = append(gpuInfos, gpuInfo)
		}
	}
	
	return gpuInfos, nil
}

// GetAvailableGPUCapacity returns the available GPU capacity across nodes
func (d *nodeGPUDetector) GetAvailableGPUCapacity(ctx context.Context, nodeNames []string) (*GPUCapacity, error) {
	gpuInfos, err := d.DetectGPUInfo(ctx, nodeNames)
	if err != nil {
		return nil, err
	}
	
	if len(gpuInfos) == 0 {
		return nil, fmt.Errorf("no GPU information found from nodes")
	}
	
	capacity := &GPUCapacity{
		GPUNodes: make(map[string]*GPUInfo),
	}
	
	for _, gpuInfo := range gpuInfos {
		capacity.TotalGPUCount += gpuInfo.GPUCount
		capacity.TotalGPUMemoryGiB += gpuInfo.GPUMemoryGiB
		capacity.GPUNodes[gpuInfo.NodeName] = gpuInfo
		
		// Track the highest GPU memory per node
		if gpuInfo.GPUMemoryGiB > capacity.HighestGPUMemoryGiB {
			capacity.HighestGPUMemoryGiB = gpuInfo.GPUMemoryGiB
		}
	}
	
	// Calculate per-GPU memory (assume uniform distribution)
	if capacity.TotalGPUCount > 0 {
		capacity.PerGPUMemoryGiB = capacity.TotalGPUMemoryGiB / float64(capacity.TotalGPUCount)
	}
	
	return capacity, nil
}

// detectGPUFromNode detects GPU information from a single node
func (d *nodeGPUDetector) detectGPUFromNode(node *corev1.Node) *GPUInfo {
	if node == nil {
		return nil
	}
	
	// Try different detection methods in order of preference
	methods := []func(*corev1.Node) *GPUInfo{
		d.detectFromNodeLabels,
		d.detectFromNodeCapacity,
		d.detectFromNodeStatus,
	}
	
	for _, method := range methods {
		if gpuInfo := method(node); gpuInfo != nil {
			gpuInfo.NodeName = node.Name
			return gpuInfo
		}
	}
	
	return nil
}

// detectFromNodeLabels detects GPU info from node labels (most detailed)
func (d *nodeGPUDetector) detectFromNodeLabels(node *corev1.Node) *GPUInfo {
	labels := node.Labels
	
	// Check for NVIDIA GPU memory label
	if memoryLabel, exists := labels["nvidia.com/gpu.memory"]; exists {
		gpuMemoryMiB, err := strconv.ParseFloat(memoryLabel, 64)
		if err == nil {
			gpuInfo := &GPUInfo{
				GPUMemoryGiB:    gpuMemoryMiB / 1024, // Convert MiB to GiB
				DetectionMethod: "node_labels_nvidia",
			}
			
			// Get GPU count from capacity
			if gpuCount := d.getGPUCountFromCapacity(node); gpuCount > 0 {
				gpuInfo.GPUCount = gpuCount
				gpuInfo.GPUMemoryGiB = gpuInfo.GPUMemoryGiB * float64(gpuCount)
			}
			
			// Get GPU model
			if model, exists := labels["nvidia.com/gpu.product"]; exists {
				gpuInfo.GPUModel = model
			}
			
			// Get driver version
			if driver, exists := labels["nvidia.com/driver-version"]; exists {
				gpuInfo.GPUDriver = driver
			}
			
			return gpuInfo
		}
	}
	
	// Check for AMD GPU labels
	if memoryLabel, exists := labels["amd.com/gpu.memory"]; exists {
		gpuMemoryMiB, err := strconv.ParseFloat(memoryLabel, 64)
		if err == nil {
			gpuInfo := &GPUInfo{
				GPUMemoryGiB:    gpuMemoryMiB / 1024,
				DetectionMethod: "node_labels_amd",
			}
			
			// Get GPU count from capacity
			if gpuCount := d.getGPUCountFromCapacity(node); gpuCount > 0 {
				gpuInfo.GPUCount = gpuCount
				gpuInfo.GPUMemoryGiB = gpuInfo.GPUMemoryGiB * float64(gpuCount)
			}
			
			return gpuInfo
		}
	}
	
	// Check for generic GPU memory labels
	for labelKey, labelValue := range labels {
		if strings.Contains(labelKey, "gpu.memory") || strings.Contains(labelKey, "gpu-memory") {
			if gpuMemoryMiB, err := strconv.ParseFloat(labelValue, 64); err == nil {
				gpuInfo := &GPUInfo{
					GPUMemoryGiB:    gpuMemoryMiB / 1024,
					DetectionMethod: "node_labels_generic",
				}
				
				if gpuCount := d.getGPUCountFromCapacity(node); gpuCount > 0 {
					gpuInfo.GPUCount = gpuCount
					gpuInfo.GPUMemoryGiB = gpuInfo.GPUMemoryGiB * float64(gpuCount)
				}
				
				return gpuInfo
			}
		}
	}
	
	return nil
}

// detectFromNodeCapacity detects GPU info from node capacity (basic)
func (d *nodeGPUDetector) detectFromNodeCapacity(node *corev1.Node) *GPUInfo {
	capacity := node.Status.Capacity
	
	// Check for NVIDIA GPU capacity
	if gpuResource, exists := capacity[consts.NvidiaGPU]; exists {
		gpuCount := int(gpuResource.Value())
		if gpuCount > 0 {
			return &GPUInfo{
				GPUCount:        gpuCount,
				GPUMemoryGiB:    0, // Unknown from capacity alone
				DetectionMethod: "node_capacity_nvidia",
			}
		}
	}
	
	// Check for AMD GPU capacity
	if gpuResource, exists := capacity["amd.com/gpu"]; exists {
		gpuCount := int(gpuResource.Value())
		if gpuCount > 0 {
			return &GPUInfo{
				GPUCount:        gpuCount,
				GPUMemoryGiB:    0, // Unknown from capacity alone
				DetectionMethod: "node_capacity_amd",
			}
		}
	}
	
	// Check for generic GPU capacity
	for resourceName, quantity := range capacity {
		if strings.Contains(string(resourceName), "gpu") {
			gpuCount := int(quantity.Value())
			if gpuCount > 0 {
				return &GPUInfo{
					GPUCount:        gpuCount,
					GPUMemoryGiB:    0, // Unknown from capacity alone
					DetectionMethod: "node_capacity_generic",
				}
			}
		}
	}
	
	return nil
}

// detectFromNodeStatus detects GPU info from node status and annotations
func (d *nodeGPUDetector) detectFromNodeStatus(node *corev1.Node) *GPUInfo {
	// Check node annotations for GPU information
	annotations := node.Annotations
	
	// Check for node feature discovery annotations
	if nfdLabels, exists := annotations["nfd.node.kubernetes.io/feature-labels"]; exists {
		if strings.Contains(nfdLabels, "gpu") {
			// Try to parse GPU information from NFD labels
			return d.parseNFDGPUInfo(nfdLabels)
		}
	}
	
	// Check for cloud provider specific annotations
	if instanceType, exists := annotations["node.kubernetes.io/instance-type"]; exists {
		return d.getGPUInfoFromInstanceType(instanceType)
	}
	
	return nil
}

// parseNFDGPUInfo parses GPU information from Node Feature Discovery labels
func (d *nodeGPUDetector) parseNFDGPUInfo(nfdLabels string) *GPUInfo {
	// This is a simplified implementation - in practice, you'd need more robust parsing
	if strings.Contains(nfdLabels, "nvidia") {
		return &GPUInfo{
			GPUCount:        1, // Default assumption
			GPUMemoryGiB:    0, // Unknown
			GPUModel:        "nvidia",
			DetectionMethod: "nfd_labels",
		}
	}
	
	if strings.Contains(nfdLabels, "amd") {
		return &GPUInfo{
			GPUCount:        1, // Default assumption
			GPUMemoryGiB:    0, // Unknown
			GPUModel:        "amd",
			DetectionMethod: "nfd_labels",
		}
	}
	
	return nil
}

// getGPUInfoFromInstanceType gets GPU info from cloud provider instance type
func (d *nodeGPUDetector) getGPUInfoFromInstanceType(instanceType string) *GPUInfo {
	// Try to get GPU config from known SKUs
	if gpuConfig := d.getGPUConfigFromSKU(instanceType); gpuConfig != nil {
		return &GPUInfo{
			GPUCount:        gpuConfig.GPUCount,
			GPUMemoryGiB:    float64(gpuConfig.GPUMemGB),
			GPUModel:        gpuConfig.GPUModel,
			DetectionMethod: "cloud_sku",
		}
	}
	
	return nil
}

// getGPUConfigFromSKU gets GPU config from known SKU configurations
func (d *nodeGPUDetector) getGPUConfigFromSKU(instanceType string) *sku.GPUConfig {
	// Try different cloud providers
	handlers := []sku.CloudSKUHandler{
		sku.NewAzureSKUHandler(),
		sku.NewArcSKUHandler(),
		// Add more handlers as needed
	}
	
	for _, handler := range handlers {
		if config := handler.GetGPUConfigBySKU(instanceType); config != nil {
			return config
		}
	}
	
	return nil
}

// getGPUCountFromCapacity gets GPU count from node capacity
func (d *nodeGPUDetector) getGPUCountFromCapacity(node *corev1.Node) int {
	capacity := node.Status.Capacity
	
	// Check for NVIDIA GPU capacity
	if gpuResource, exists := capacity[consts.NvidiaGPU]; exists {
		return int(gpuResource.Value())
	}
	
	// Check for AMD GPU capacity
	if gpuResource, exists := capacity["amd.com/gpu"]; exists {
		return int(gpuResource.Value())
	}
	
	// Check for generic GPU capacity
	for resourceName, quantity := range capacity {
		if strings.Contains(string(resourceName), "gpu") {
			return int(quantity.Value())
		}
	}
	
	return 0
}

// getNode retrieves a node by name
func (d *nodeGPUDetector) getNode(ctx context.Context, nodeName string) (*corev1.Node, error) {
	nodeList := &corev1.NodeList{}
	fieldSelector := fields.OneTermEqualSelector("metadata.name", nodeName)
	
	err := d.kubeClient.List(ctx, nodeList, &client.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return nil, err
	}
	
	if len(nodeList.Items) == 0 {
		return nil, fmt.Errorf("node %s not found", nodeName)
	}
	
	return &nodeList.Items[0], nil
}

// CanSatisfyRequirement checks if the GPU capacity can satisfy the VRAM requirement
func (gc *GPUCapacity) CanSatisfyRequirement(vramReq *VRAMRequirement) bool {
	// Check if we have enough total GPU memory
	if gc.TotalGPUMemoryGiB < vramReq.TotalGPUMemoryGiB {
		return false
	}
	
	// Check if we have enough GPUs
	if gc.TotalGPUCount < vramReq.MinGPUCount {
		return false
	}
	
	// Check if per-GPU memory is sufficient (for distributed scenarios)
	if vramReq.PerGPUMemoryGiB > 0 && gc.PerGPUMemoryGiB < vramReq.PerGPUMemoryGiB {
		return false
	}
	
	return true
}

// GetUtilizationPercentage calculates the utilization percentage if the requirement is satisfied
func (gc *GPUCapacity) GetUtilizationPercentage(vramReq *VRAMRequirement) float64 {
	if gc.TotalGPUMemoryGiB == 0 {
		return 0
	}
	
	return (vramReq.TotalGPUMemoryGiB / gc.TotalGPUMemoryGiB) * 100
}

// String returns a human-readable representation of the GPU capacity
func (gc *GPUCapacity) String() string {
	return fmt.Sprintf("Total GPUs: %d, Total Memory: %.2f GiB, Per-GPU Memory: %.2f GiB, Nodes: %d",
		gc.TotalGPUCount, gc.TotalGPUMemoryGiB, gc.PerGPUMemoryGiB, len(gc.GPUNodes))
}

// String returns a human-readable representation of the GPU info
func (gi *GPUInfo) String() string {
	return fmt.Sprintf("Node: %s, GPUs: %d, Memory: %.2f GiB, Model: %s, Method: %s",
		gi.NodeName, gi.GPUCount, gi.GPUMemoryGiB, gi.GPUModel, gi.DetectionMethod)
}
