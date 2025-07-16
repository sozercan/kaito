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
	"strconv"
	"strings"
	"testing"

	"github.com/kaito-project/kaito/pkg/model"
)

func TestVRAMCalculatorBasic(t *testing.T) {
	calculator := NewVRAMCalculator()
	
	// Test a simple model configuration
	config := &ModelConfig{
		ParameterCount: 7_000_000_000,
		Precision:      "fp16",
		SequenceLength: 2048,
		BatchSize:      1,
		Runtime:        model.RuntimeNameVLLM,
		HiddenSize:     4096,
		NumLayers:      32,
		ModelType:      "llama",
	}
	
	result, err := calculator.CalculateVRAMRequirement(nil, "test-model", config)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	
	if result == nil {
		t.Errorf("Expected result but got nil")
	}
	
	if result.MinGPUCount <= 0 {
		t.Errorf("Expected positive GPU count, got %d", result.MinGPUCount)
	}
	
	if result.TotalGPUMemoryGiB <= 0 {
		t.Errorf("Expected positive memory requirement, got %.2f", result.TotalGPUMemoryGiB)
	}
	
	if result.ConfidenceLevel == "" {
		t.Errorf("Expected confidence level to be set")
	}
}

func TestVRAMCalculatorQuantized(t *testing.T) {
	calculator := NewVRAMCalculator()
	
	// Test quantized model
	config := &ModelConfig{
		ParameterCount:      7_000_000_000,
		Precision:           "fp16",
		SequenceLength:      2048,
		BatchSize:           1,
		Runtime:             model.RuntimeNameVLLM,
		HiddenSize:          4096,
		NumLayers:           32,
		ModelType:           "llama",
		QuantizationEnabled: true,
		QuantizationBits:    8,
	}
	
	result, err := calculator.CalculateVRAMRequirement(nil, "test-model-quantized", config)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	
	if result == nil {
		t.Errorf("Expected result but got nil")
	}
	
	// Quantized model should require less memory
	if result.TotalGPUMemoryGiB > 15.0 {
		t.Errorf("Expected quantized model to require less memory, got %.2f", result.TotalGPUMemoryGiB)
	}
}

func TestCreateModelConfigFromPreset(t *testing.T) {
	tests := []struct {
		name           string
		presetName     string
		runtime        model.RuntimeName
		expectedType   string
		expectedParams int64
	}{
		{
			name:           "Llama 7B model",
			presetName:     "llama-7b",
			runtime:        model.RuntimeNameVLLM,
			expectedType:   "llama",
			expectedParams: 7_000_000_000,
		},
		{
			name:           "Phi model",
			presetName:     "phi-3-mini",
			runtime:        model.RuntimeNameVLLM,
			expectedType:   "phi",
			expectedParams: 14_000_000_000,
		},
		{
			name:           "Mistral model",
			presetName:     "mistral-7b",
			runtime:        model.RuntimeNameVLLM,
			expectedType:   "mistral",
			expectedParams: 7_000_000_000,
		},
		{
			name:           "Llama 70B model",
			presetName:     "llama-70b",
			runtime:        model.RuntimeNameVLLM,
			expectedType:   "llama",
			expectedParams: 70_000_000_000,
		},
		{
			name:           "Unknown model defaults to zero",
			presetName:     "unknown-model",
			runtime:        model.RuntimeNameVLLM,
			expectedType:   "",
			expectedParams: 0,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := CreateModelConfigFromPreset(tt.presetName, tt.runtime)
			
			if config.ModelType != tt.expectedType {
				t.Errorf("Expected model type %s, got %s", tt.expectedType, config.ModelType)
			}
			
			if config.Runtime != tt.runtime {
				t.Errorf("Expected runtime %s, got %s", tt.runtime, config.Runtime)
			}
			
			if config.ParameterCount != tt.expectedParams {
				t.Errorf("Expected parameter count %d, got %d", tt.expectedParams, config.ParameterCount)
			}
		})
	}
}

func TestVRAMRequirementValidation(t *testing.T) {
	requirement := &VRAMRequirement{
		TotalGPUMemoryGiB: 16.0,
		PerGPUMemoryGiB:   8.0,
		MinGPUCount:       2,
		ConfidenceLevel:   "high",
	}
	
	// Test conversion to resource quantity
	quantity, err := requirement.ConvertToResourceQuantity()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	
	expectedBytes := int64(16.0 * 1024 * 1024 * 1024) // 16 GiB
	if quantity.Value() != expectedBytes {
		t.Errorf("Expected %d bytes, got %d", expectedBytes, quantity.Value())
	}
	
	// Test per-GPU conversion
	perGPU, err := requirement.ConvertPerGPUToResourceQuantity()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	
	expectedPerGPUBytes := int64(8.0 * 1024 * 1024 * 1024) // 8 GiB
	if perGPU.Value() != expectedPerGPUBytes {
		t.Errorf("Expected %d bytes per GPU, got %d", expectedPerGPUBytes, perGPU.Value())
	}
}

func TestGPUCapacityBasics(t *testing.T) {
	capacity := &GPUCapacity{
		TotalGPUCount:     4,
		TotalGPUMemoryGiB: 320.0,
		PerGPUMemoryGiB:   80.0,
		GPUNodes:          make(map[string]*GPUInfo),
	}
	
	// Test satisfiable requirement
	requirement := &VRAMRequirement{
		TotalGPUMemoryGiB: 160.0,
		PerGPUMemoryGiB:   40.0,
		MinGPUCount:       2,
	}
	
	if !capacity.CanSatisfyRequirement(requirement) {
		t.Errorf("Expected capacity to satisfy requirement")
	}
	
	// Test unsatisfiable requirement
	impossibleRequirement := &VRAMRequirement{
		TotalGPUMemoryGiB: 400.0,
		PerGPUMemoryGiB:   80.0,
		MinGPUCount:       6,
	}
	
	if capacity.CanSatisfyRequirement(impossibleRequirement) {
		t.Errorf("Expected capacity to NOT satisfy impossible requirement")
	}
}

func TestMemoryParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
	}{
		{"16Gi", 16.0},
		{"80GiB", 80.0},
		{"32G", 32.0},
		{"24GB", 24.0},
		{"16", 16.0},
		{"invalid", 0.0},
		{"", 0.0},
	}
	
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseMemoryString(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %.2f, got %.2f", tt.expected, result)
			}
		})
	}
}

// Helper function for testing
func parseMemoryString(memoryStr string) float64 {
	if memoryStr == "" {
		return 0.0
	}
	
	// Remove common suffixes
	suffixes := []string{"GiB", "Gi", "GB", "G"}
	for _, suffix := range suffixes {
		if strings.HasSuffix(memoryStr, suffix) {
			memoryStr = strings.TrimSuffix(memoryStr, suffix)
			break
		}
	}
	
	if value, err := strconv.ParseFloat(memoryStr, 64); err == nil {
		return value
	}
	
	return 0.0
}
