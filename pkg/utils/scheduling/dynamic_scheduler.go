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
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/sku"
)

// DynamicScheduler provides dynamic model scheduling based on actual GPU capabilities
type DynamicScheduler interface {
	// CanScheduleModel checks if a model can be scheduled on the given nodes
	CanScheduleModel(ctx context.Context, modelName string, modelConfig *ModelConfig, nodeNames []string) (*SchedulingResult, error)
	
	// GetOptimalScheduling returns the optimal scheduling configuration for a model
	GetOptimalScheduling(ctx context.Context, modelName string, modelConfig *ModelConfig, nodeNames []string) (*SchedulingResult, error)
	
	// ValidateScheduling validates if the proposed scheduling is feasible
	ValidateScheduling(ctx context.Context, modelName string, modelConfig *ModelConfig, proposedScheduling *ProposedScheduling) error
}

// SchedulingResult contains the result of scheduling analysis
type SchedulingResult struct {
	// Whether the model can be scheduled
	Feasible             bool
	
	// GPU requirements calculated
	VRAMRequirement      *VRAMRequirement
	
	// Available GPU capacity
	GPUCapacity          *GPUCapacity
	
	// Recommended configuration
	RecommendedGPUCount  int
	RecommendedNodes     []string
	
	// Scheduling strategy
	Strategy             string // "single_node", "multi_node", "distributed"
	
	// Utilization metrics
	MemoryUtilization    float64 // Percentage
	GPUUtilization       float64 // Percentage
	
	// Warnings and recommendations
	Warnings             []string
	Recommendations      []string
	
	// Fallback options
	FallbackOptions      []*FallbackOption
}

// ProposedScheduling represents a proposed scheduling configuration
type ProposedScheduling struct {
	NodeNames           []string
	GPUCount            int
	InstanceType        string
	MemoryRequirement   *resource.Quantity
	AllowCPUOffload     bool
}

// FallbackOption represents a fallback scheduling option
type FallbackOption struct {
	Description         string
	RequiredChanges     []string
	EstimatedResources  *VRAMRequirement
	ConfidenceLevel     string
}

// dynamicScheduler implements DynamicScheduler
type dynamicScheduler struct {
	kubeClient      client.Client
	vramCalculator  VRAMCalculator
	gpuDetector     GPUDetector
}

// NewDynamicScheduler creates a new dynamic scheduler
func NewDynamicScheduler(kubeClient client.Client) DynamicScheduler {
	return &dynamicScheduler{
		kubeClient:     kubeClient,
		vramCalculator: NewVRAMCalculator(),
		gpuDetector:    NewGPUDetector(kubeClient),
	}
}

// CanScheduleModel checks if a model can be scheduled on the given nodes
func (ds *dynamicScheduler) CanScheduleModel(ctx context.Context, modelName string, modelConfig *ModelConfig, nodeNames []string) (*SchedulingResult, error) {
	// Calculate VRAM requirements
	vramReq, err := ds.vramCalculator.CalculateVRAMRequirement(ctx, modelName, modelConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate VRAM requirement: %w", err)
	}
	
	// Detect available GPU capacity
	gpuCapacity, err := ds.gpuDetector.GetAvailableGPUCapacity(ctx, nodeNames)
	if err != nil {
		return nil, fmt.Errorf("failed to detect GPU capacity: %w", err)
	}
	
	// Analyze scheduling feasibility
	result := ds.analyzeSchedulingFeasibility(vramReq, gpuCapacity, nodeNames)
	result.VRAMRequirement = vramReq
	result.GPUCapacity = gpuCapacity
	
	return result, nil
}

// GetOptimalScheduling returns the optimal scheduling configuration for a model
func (ds *dynamicScheduler) GetOptimalScheduling(ctx context.Context, modelName string, modelConfig *ModelConfig, nodeNames []string) (*SchedulingResult, error) {
	result, err := ds.CanScheduleModel(ctx, modelName, modelConfig, nodeNames)
	if err != nil {
		return nil, err
	}
	
	if !result.Feasible {
		// Generate fallback options
		result.FallbackOptions = ds.generateFallbackOptions(result.VRAMRequirement, result.GPUCapacity)
	}
	
	return result, nil
}

// ValidateScheduling validates if the proposed scheduling is feasible
func (ds *dynamicScheduler) ValidateScheduling(ctx context.Context, modelName string, modelConfig *ModelConfig, proposedScheduling *ProposedScheduling) error {
	// Calculate VRAM requirements
	vramReq, err := ds.vramCalculator.CalculateVRAMRequirement(ctx, modelName, modelConfig)
	if err != nil {
		return fmt.Errorf("failed to calculate VRAM requirement: %w", err)
	}
	
	// Detect available GPU capacity from proposed nodes
	gpuCapacity, err := ds.gpuDetector.GetAvailableGPUCapacity(ctx, proposedScheduling.NodeNames)
	if err != nil {
		return fmt.Errorf("failed to detect GPU capacity: %w", err)
	}
	
	// Validate the proposed scheduling
	if !gpuCapacity.CanSatisfyRequirement(vramReq) {
		return fmt.Errorf("proposed scheduling cannot satisfy VRAM requirement: need %.2f GiB but only %.2f GiB available",
			vramReq.TotalGPUMemoryGiB, gpuCapacity.TotalGPUMemoryGiB)
	}
	
	return nil
}

// analyzeSchedulingFeasibility analyzes whether scheduling is feasible
func (ds *dynamicScheduler) analyzeSchedulingFeasibility(vramReq *VRAMRequirement, gpuCapacity *GPUCapacity, nodeNames []string) *SchedulingResult {
	result := &SchedulingResult{
		RecommendedNodes: nodeNames,
		Warnings:         []string{},
		Recommendations:  []string{},
	}
	
	// Check basic feasibility
	result.Feasible = gpuCapacity.CanSatisfyRequirement(vramReq)
	
	// Calculate utilization metrics
	result.MemoryUtilization = gpuCapacity.GetUtilizationPercentage(vramReq)
	result.GPUUtilization = float64(vramReq.MinGPUCount) / float64(gpuCapacity.TotalGPUCount) * 100
	
	// Determine scheduling strategy
	result.Strategy = ds.determineSchedulingStrategy(vramReq, gpuCapacity)
	result.RecommendedGPUCount = ds.calculateRecommendedGPUCount(vramReq, gpuCapacity)
	
	// Generate warnings and recommendations
	result.Warnings = ds.generateWarnings(vramReq, gpuCapacity)
	result.Recommendations = ds.generateRecommendations(vramReq, gpuCapacity)
	
	return result
}

// determineSchedulingStrategy determines the optimal scheduling strategy
func (ds *dynamicScheduler) determineSchedulingStrategy(vramReq *VRAMRequirement, gpuCapacity *GPUCapacity) string {
	// Single node strategy
	if vramReq.MinGPUCount == 1 && len(gpuCapacity.GPUNodes) == 1 {
		return "single_node"
	}
	
	// Check if the model can fit on a single node
	if vramReq.TotalGPUMemoryGiB <= gpuCapacity.HighestGPUMemoryGiB {
		return "single_node"
	}
	
	// Multi-node strategy
	if vramReq.MinGPUCount > 1 && vramReq.MinGPUCount <= gpuCapacity.TotalGPUCount {
		return "multi_node"
	}
	
	// Distributed strategy for large models
	return "distributed"
}

// calculateRecommendedGPUCount calculates the recommended GPU count
func (ds *dynamicScheduler) calculateRecommendedGPUCount(vramReq *VRAMRequirement, gpuCapacity *GPUCapacity) int {
	// Start with minimum required GPUs
	recommendedCount := vramReq.MinGPUCount
	
	// Adjust based on available capacity
	if recommendedCount > gpuCapacity.TotalGPUCount {
		recommendedCount = gpuCapacity.TotalGPUCount
	}
	
	// Optimize for memory utilization
	if gpuCapacity.PerGPUMemoryGiB > 0 {
		optimalCount := int(vramReq.TotalGPUMemoryGiB / gpuCapacity.PerGPUMemoryGiB)
		if optimalCount > recommendedCount {
			recommendedCount = optimalCount
		}
	}
	
	return recommendedCount
}

// generateWarnings generates warnings about the scheduling
func (ds *dynamicScheduler) generateWarnings(vramReq *VRAMRequirement, gpuCapacity *GPUCapacity) []string {
	var warnings []string
	
	// Low confidence warning
	if vramReq.ConfidenceLevel == "low" {
		warnings = append(warnings, "VRAM requirement estimation has low confidence due to limited model information")
	}
	
	// High utilization warning
	if gpuCapacity.GetUtilizationPercentage(vramReq) > 90 {
		warnings = append(warnings, "High GPU memory utilization (>90%) may cause OOM errors")
	}
	
	// Insufficient GPU count warning
	if vramReq.MinGPUCount > gpuCapacity.TotalGPUCount {
		warnings = append(warnings, fmt.Sprintf("Model requires %d GPUs but only %d available", 
			vramReq.MinGPUCount, gpuCapacity.TotalGPUCount))
	}
	
	// Per-GPU memory warning
	if vramReq.PerGPUMemoryGiB > gpuCapacity.PerGPUMemoryGiB {
		warnings = append(warnings, fmt.Sprintf("Model requires %.2f GiB per GPU but only %.2f GiB available per GPU",
			vramReq.PerGPUMemoryGiB, gpuCapacity.PerGPUMemoryGiB))
	}
	
	return warnings
}

// generateRecommendations generates scheduling recommendations
func (ds *dynamicScheduler) generateRecommendations(vramReq *VRAMRequirement, gpuCapacity *GPUCapacity) []string {
	var recommendations []string
	
	// Quantization recommendation
	if vramReq.TotalGPUMemoryGiB > gpuCapacity.TotalGPUMemoryGiB {
		recommendations = append(recommendations, "Consider enabling quantization (int8/int4) to reduce memory requirements")
	}
	
	// CPU offload recommendation
	if vramReq.TotalGPUMemoryGiB > gpuCapacity.TotalGPUMemoryGiB * 0.8 {
		recommendations = append(recommendations, "Consider enabling CPU offload for model weights")
	}
	
	// Sequence length recommendation
	if vramReq.CalculationMethod == "model_parameters" {
		recommendations = append(recommendations, "Consider reducing max sequence length to decrease KV cache memory usage")
	}
	
	// GPU upgrade recommendation
	if vramReq.PerGPUMemoryGiB > gpuCapacity.PerGPUMemoryGiB {
		recommendations = append(recommendations, "Consider upgrading to GPUs with higher memory capacity")
	}
	
	return recommendations
}

// generateFallbackOptions generates fallback scheduling options
func (ds *dynamicScheduler) generateFallbackOptions(vramReq *VRAMRequirement, gpuCapacity *GPUCapacity) []*FallbackOption {
	var options []*FallbackOption
	
	// Quantization fallback
	if vramReq.TotalGPUMemoryGiB > gpuCapacity.TotalGPUMemoryGiB {
		quantizedReq := *vramReq
		quantizedReq.TotalGPUMemoryGiB = vramReq.TotalGPUMemoryGiB * 0.5 // Assume 50% reduction with int8
		quantizedReq.PerGPUMemoryGiB = vramReq.PerGPUMemoryGiB * 0.5
		
		options = append(options, &FallbackOption{
			Description:        "Enable INT8 quantization",
			RequiredChanges:    []string{"Set quantization_bits: 8 in model config"},
			EstimatedResources: &quantizedReq,
			ConfidenceLevel:    "medium",
		})
	}
	
	// CPU offload fallback
	if vramReq.TotalGPUMemoryGiB > gpuCapacity.TotalGPUMemoryGiB * 0.7 {
		offloadReq := *vramReq
		offloadReq.TotalGPUMemoryGiB = gpuCapacity.TotalGPUMemoryGiB * 0.9 // Use 90% of available GPU memory
		
		options = append(options, &FallbackOption{
			Description:        "Enable CPU offload",
			RequiredChanges:    []string{"Set cpu_offload: true in workspace config"},
			EstimatedResources: &offloadReq,
			ConfidenceLevel:    "medium",
		})
	}
	
	// Reduced sequence length fallback
	reducedSeqReq := *vramReq
	reducedSeqReq.KVCacheGiB = vramReq.KVCacheGiB * 0.5 // Assume 50% reduction
	reducedSeqReq.TotalGPUMemoryGiB = vramReq.TotalGPUMemoryGiB - vramReq.KVCacheGiB + reducedSeqReq.KVCacheGiB
	
	options = append(options, &FallbackOption{
		Description:        "Reduce maximum sequence length",
		RequiredChanges:    []string{"Set max_model_len: 2048 in inference config"},
		EstimatedResources: &reducedSeqReq,
		ConfidenceLevel:    "high",
	})
	
	return options
}

// CreateModelConfigFromPreset creates a ModelConfig from a preset model
func CreateModelConfigFromPreset(presetName string, runtime model.RuntimeName) *ModelConfig {
	config := &ModelConfig{
		Runtime:         runtime,
		Precision:       "fp16", // Default precision
		SequenceLength:  2048,   // Default sequence length
		BatchSize:       1,      // Default batch size
	}
	
	// Extract model size from preset name
	modelName := strings.ToLower(presetName)
	
	// Set model parameters based on common preset patterns
	switch {
	case strings.Contains(modelName, "phi"):
		config.ParameterCount = 14_000_000_000
		config.HiddenSize = 5120
		config.NumLayers = 40
		config.AttentionHeads = 40
		config.ModelType = "phi"
	case strings.Contains(modelName, "mistral"):
		config.ParameterCount = 7_000_000_000
		config.HiddenSize = 4096
		config.NumLayers = 32
		config.AttentionHeads = 32
		config.ModelType = "mistral"
	case strings.Contains(modelName, "70b"):
		config.ParameterCount = 70_000_000_000
		config.HiddenSize = 8192
		config.NumLayers = 80
		config.AttentionHeads = 64
		config.ModelType = "llama"
	case strings.Contains(modelName, "13b"):
		config.ParameterCount = 13_000_000_000
		config.HiddenSize = 5120
		config.NumLayers = 40
		config.AttentionHeads = 40
		config.ModelType = "llama"
	case strings.Contains(modelName, "7b"):
		config.ParameterCount = 7_000_000_000
		config.HiddenSize = 4096
		config.NumLayers = 32
		config.AttentionHeads = 32
		config.ModelType = "llama"
	}
	
	return config
}

// CreateModelConfigFromSKUAndPreset creates a ModelConfig using both SKU and preset information
func CreateModelConfigFromSKUAndPreset(presetName string, runtime model.RuntimeName, skuConfig *sku.GPUConfig) *ModelConfig {
	config := CreateModelConfigFromPreset(presetName, runtime)
	
	// Adjust configuration based on SKU capabilities
	if skuConfig != nil {
		// Adjust sequence length based on GPU memory
		if skuConfig.GPUMemGB >= 80 {
			config.SequenceLength = 8192 // Higher sequence length for high-memory GPUs
		} else if skuConfig.GPUMemGB >= 40 {
			config.SequenceLength = 4096
		} else {
			config.SequenceLength = 2048
		}
		
		// Enable quantization for lower memory GPUs
		if skuConfig.GPUMemGB < 40 {
			config.QuantizationEnabled = true
			config.QuantizationBits = 8
		}
	}
	
	return config
}

// String returns a human-readable representation of the scheduling result
func (sr *SchedulingResult) String() string {
	feasible := "No"
	if sr.Feasible {
		feasible = "Yes"
	}
	
	return fmt.Sprintf("Feasible: %s, Strategy: %s, GPUs: %d, Memory Util: %.1f%%, GPU Util: %.1f%%",
		feasible, sr.Strategy, sr.RecommendedGPUCount, sr.MemoryUtilization, sr.GPUUtilization)
}
