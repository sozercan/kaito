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
	"math"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/kaito-project/kaito/pkg/model"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

// VRAMCalculator provides methods to calculate GPU VRAM requirements for models
type VRAMCalculator interface {
	// CalculateVRAMRequirement calculates the GPU VRAM needed for a model
	CalculateVRAMRequirement(ctx context.Context, modelName string, modelConfig *ModelConfig) (*VRAMRequirement, error)
}

// ModelConfig contains the configuration parameters for a model
type ModelConfig struct {
	// Model parameters
	ParameterCount       int64  // Number of parameters in the model (e.g., 7B, 13B)
	Precision            string // Model precision (fp16, bf16, fp32, int8, int4)
	SequenceLength       int    // Maximum sequence length
	BatchSize            int    // Batch size for inference
	
	// Runtime configuration
	Runtime              model.RuntimeName // Runtime (transformers, vllm)
	QuantizationEnabled  bool             // Whether quantization is enabled
	QuantizationBits     int              // Quantization bits (4, 8, 16)
	
	// Model architecture hints
	ModelType            string           // Model type (llama, mistral, etc.)
	AttentionHeads       int              // Number of attention heads
	HiddenSize           int              // Hidden dimension size
	NumLayers            int              // Number of transformer layers
	VocabSize            int              // Vocabulary size
}

// VRAMRequirement represents the calculated GPU VRAM requirements
type VRAMRequirement struct {
	// Total GPU memory needed
	TotalGPUMemoryGiB    float64
	// Per-GPU memory needed (for distributed inference)
	PerGPUMemoryGiB      float64
	// Minimum number of GPUs required
	MinGPUCount          int
	// Memory breakdown
	ModelWeightsGiB      float64
	KVCacheGiB           float64
	ActivationMemoryGiB  float64
	ReservedMemoryGiB    float64
	// Confidence level of the estimation
	ConfidenceLevel      string // "high", "medium", "low"
	// Calculation method used
	CalculationMethod    string
}

// dynamicVRAMCalculator implements VRAMCalculator with dynamic calculation logic
type dynamicVRAMCalculator struct{}

// NewVRAMCalculator creates a new VRAM calculator instance
func NewVRAMCalculator() VRAMCalculator {
	return &dynamicVRAMCalculator{}
}

// CalculateVRAMRequirement calculates the GPU VRAM requirements for a model
func (c *dynamicVRAMCalculator) CalculateVRAMRequirement(ctx context.Context, modelName string, modelConfig *ModelConfig) (*VRAMRequirement, error) {
	if modelConfig == nil {
		return nil, fmt.Errorf("model config cannot be nil")
	}

	// Try different calculation methods in order of preference
	methods := []func(context.Context, string, *ModelConfig) (*VRAMRequirement, error){
		c.calculateFromModelParameters,
		c.calculateFromHeuristics,
		c.calculateFromDefaults,
	}

	var lastErr error
	for _, method := range methods {
		result, err := method(ctx, modelName, modelConfig)
		if err != nil {
			lastErr = err
			continue
		}
		if result != nil {
			return result, nil
		}
	}

	return nil, fmt.Errorf("failed to calculate VRAM requirement: %v", lastErr)
}

// calculateFromModelParameters calculates VRAM based on detailed model parameters
func (c *dynamicVRAMCalculator) calculateFromModelParameters(ctx context.Context, modelName string, modelConfig *ModelConfig) (*VRAMRequirement, error) {
	if modelConfig.ParameterCount == 0 {
		return nil, fmt.Errorf("parameter count not available")
	}

	// Calculate model weights memory
	modelWeightsMemory := c.calculateModelWeightsMemory(modelConfig)
	
	// Calculate KV cache memory
	kvCacheMemory := c.calculateKVCacheMemory(modelConfig)
	
	// Calculate activation memory
	activationMemory := c.calculateActivationMemory(modelConfig)
	
	// Reserved memory for other operations
	reservedMemory := 1.5 // 1.5 GiB as default
	
	// Total memory needed
	totalMemory := modelWeightsMemory + kvCacheMemory + activationMemory + reservedMemory
	
	// Calculate minimum GPUs needed and per-GPU memory
	minGPUs, perGPUMemory := c.calculateDistributionStrategy(totalMemory, modelConfig)
	
	return &VRAMRequirement{
		TotalGPUMemoryGiB:   totalMemory,
		PerGPUMemoryGiB:     perGPUMemory,
		MinGPUCount:         minGPUs,
		ModelWeightsGiB:     modelWeightsMemory,
		KVCacheGiB:          kvCacheMemory,
		ActivationMemoryGiB: activationMemory,
		ReservedMemoryGiB:   reservedMemory,
		ConfidenceLevel:     "high",
		CalculationMethod:   "model_parameters",
	}, nil
}

// calculateFromHeuristics calculates VRAM using heuristic rules
func (c *dynamicVRAMCalculator) calculateFromHeuristics(ctx context.Context, modelName string, modelConfig *ModelConfig) (*VRAMRequirement, error) {
	// Parse model name to extract size information
	parameterCount := c.parseModelSizeFromName(modelName)
	if parameterCount == 0 {
		return nil, fmt.Errorf("cannot parse model size from name: %s", modelName)
	}

	// Create a config with estimated parameters
	estimatedConfig := *modelConfig
	estimatedConfig.ParameterCount = parameterCount
	
	// Use heuristic rules for common model architectures
	return c.calculateFromModelParameters(ctx, modelName, &estimatedConfig)
}

// calculateFromDefaults provides fallback calculation using conservative defaults
func (c *dynamicVRAMCalculator) calculateFromDefaults(ctx context.Context, modelName string, modelConfig *ModelConfig) (*VRAMRequirement, error) {
	// Conservative default: assume 7B parameters if nothing else is available
	defaultParams := int64(7_000_000_000)
	defaultConfig := *modelConfig
	defaultConfig.ParameterCount = defaultParams
	
	if defaultConfig.SequenceLength == 0 {
		defaultConfig.SequenceLength = 2048
	}
	if defaultConfig.BatchSize == 0 {
		defaultConfig.BatchSize = 1
	}
	
	result, err := c.calculateFromModelParameters(ctx, modelName, &defaultConfig)
	if err != nil {
		return nil, err
	}
	
	result.ConfidenceLevel = "low"
	result.CalculationMethod = "conservative_defaults"
	
	return result, nil
}

// calculateModelWeightsMemory calculates memory needed for model weights
func (c *dynamicVRAMCalculator) calculateModelWeightsMemory(config *ModelConfig) float64 {
	bytesPerParam := c.getBytesPerParameter(config)
	return float64(config.ParameterCount) * bytesPerParam / (1024 * 1024 * 1024) // Convert to GiB
}

// calculateKVCacheMemory calculates memory needed for KV cache
func (c *dynamicVRAMCalculator) calculateKVCacheMemory(config *ModelConfig) float64 {
	if config.SequenceLength == 0 || config.BatchSize == 0 {
		return 0.5 // Default 0.5 GiB if we can't calculate
	}
	
	// KV cache memory = batch_size * sequence_length * hidden_size * num_layers * 2 (for K and V) * bytes_per_element
	hiddenSize := config.HiddenSize
	if hiddenSize == 0 {
		// Estimate based on parameter count
		hiddenSize = c.estimateHiddenSize(config.ParameterCount)
	}
	
	numLayers := config.NumLayers
	if numLayers == 0 {
		numLayers = c.estimateNumLayers(config.ParameterCount)
	}
	
	bytesPerElement := c.getBytesPerParameter(config)
	kvCacheBytes := float64(config.BatchSize * config.SequenceLength * hiddenSize * numLayers * 2) * bytesPerElement
	
	return kvCacheBytes / (1024 * 1024 * 1024) // Convert to GiB
}

// calculateActivationMemory calculates memory needed for activations
func (c *dynamicVRAMCalculator) calculateActivationMemory(config *ModelConfig) float64 {
	if config.BatchSize == 0 || config.SequenceLength == 0 {
		return 0.5 // Default 0.5 GiB
	}
	
	// Activation memory is roughly proportional to batch size and sequence length
	hiddenSize := config.HiddenSize
	if hiddenSize == 0 {
		hiddenSize = c.estimateHiddenSize(config.ParameterCount)
	}
	
	bytesPerElement := c.getBytesPerParameter(config)
	activationBytes := float64(config.BatchSize * config.SequenceLength * hiddenSize * 4) * bytesPerElement // 4x multiplier for temporary activations
	
	return activationBytes / (1024 * 1024 * 1024) // Convert to GiB
}

// calculateDistributionStrategy determines how to distribute the model across GPUs
func (c *dynamicVRAMCalculator) calculateDistributionStrategy(totalMemoryGiB float64, config *ModelConfig) (int, float64) {
	// For now, assume a maximum of 80 GiB per GPU (A100 standard)
	// This can be made configurable or dynamic based on actual GPU detection
	maxMemoryPerGPU := 80.0
	
	// Calculate minimum GPUs needed
	minGPUs := int(math.Ceil(totalMemoryGiB / maxMemoryPerGPU))
	if minGPUs == 0 {
		minGPUs = 1
	}
	
	// Calculate per-GPU memory
	perGPUMemory := totalMemoryGiB / float64(minGPUs)
	
	return minGPUs, perGPUMemory
}

// getBytesPerParameter returns the number of bytes per parameter based on precision
func (c *dynamicVRAMCalculator) getBytesPerParameter(config *ModelConfig) float64 {
	if config.QuantizationEnabled {
		switch config.QuantizationBits {
		case 4:
			return 0.5
		case 8:
			return 1.0
		default:
			return 2.0 // Default to 16-bit
		}
	}
	
	switch config.Precision {
	case "fp32":
		return 4.0
	case "fp16", "bf16":
		return 2.0
	case "int8":
		return 1.0
	case "int4":
		return 0.5
	default:
		return 2.0 // Default to 16-bit
	}
}

// parseModelSizeFromName extracts parameter count from model name
func (c *dynamicVRAMCalculator) parseModelSizeFromName(modelName string) int64 {
	modelName = strings.ToLower(modelName)
	
	// Common patterns for model sizes
	patterns := map[string]int64{
		"7b":   7_000_000_000,
		"8b":   8_000_000_000,
		"13b":  13_000_000_000,
		"14b":  14_000_000_000,
		"30b":  30_000_000_000,
		"32b":  32_000_000_000,
		"40b":  40_000_000_000,
		"65b":  65_000_000_000,
		"70b":  70_000_000_000,
		"180b": 180_000_000_000,
	}
	
	for pattern, paramCount := range patterns {
		if strings.Contains(modelName, pattern) {
			return paramCount
		}
	}
	
	// Try to parse explicit numbers
	words := strings.Fields(modelName)
	for _, word := range words {
		if strings.HasSuffix(word, "b") {
			if sizeStr := strings.TrimSuffix(word, "b"); sizeStr != "" {
				if size, err := strconv.ParseFloat(sizeStr, 64); err == nil {
					return int64(size * 1_000_000_000)
				}
			}
		}
	}
	
	return 0
}

// estimateHiddenSize estimates hidden size based on parameter count
func (c *dynamicVRAMCalculator) estimateHiddenSize(paramCount int64) int {
	// Rough estimates based on common model architectures
	switch {
	case paramCount <= 1_000_000_000:
		return 1024
	case paramCount <= 7_000_000_000:
		return 4096
	case paramCount <= 13_000_000_000:
		return 5120
	case paramCount <= 30_000_000_000:
		return 6656
	case paramCount <= 70_000_000_000:
		return 8192
	default:
		return 12288
	}
}

// estimateNumLayers estimates number of layers based on parameter count
func (c *dynamicVRAMCalculator) estimateNumLayers(paramCount int64) int {
	// Rough estimates based on common model architectures
	switch {
	case paramCount <= 1_000_000_000:
		return 12
	case paramCount <= 7_000_000_000:
		return 32
	case paramCount <= 13_000_000_000:
		return 40
	case paramCount <= 30_000_000_000:
		return 60
	case paramCount <= 70_000_000_000:
		return 80
	default:
		return 100
	}
}

// ConvertToResourceQuantity converts GiB to Kubernetes resource.Quantity
func (vr *VRAMRequirement) ConvertToResourceQuantity() (*resource.Quantity, error) {
	totalBytes := int64(vr.TotalGPUMemoryGiB * consts.GiBToBytes)
	return resource.NewQuantity(totalBytes, resource.BinarySI), nil
}

// ConvertPerGPUToResourceQuantity converts per-GPU GiB to Kubernetes resource.Quantity
func (vr *VRAMRequirement) ConvertPerGPUToResourceQuantity() (*resource.Quantity, error) {
	perGPUBytes := int64(vr.PerGPUMemoryGiB * consts.GiBToBytes)
	return resource.NewQuantity(perGPUBytes, resource.BinarySI), nil
}

// String returns a human-readable representation of the VRAM requirement
func (vr *VRAMRequirement) String() string {
	return fmt.Sprintf("Total: %.2f GiB, Per-GPU: %.2f GiB, Min GPUs: %d, Confidence: %s",
		vr.TotalGPUMemoryGiB, vr.PerGPUMemoryGiB, vr.MinGPUCount, vr.ConfidenceLevel)
}
