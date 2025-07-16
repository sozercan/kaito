# Dynamic Scheduling Example

This example demonstrates the new dynamic scheduling system in KAITO that automatically calculates GPU VRAM requirements and determines optimal instance types for model deployment.

## Overview

The dynamic scheduling system consists of three main components:

1. **VRAM Calculator**: Calculates GPU memory requirements based on model parameters
2. **GPU Detector**: Detects available GPU capacity from cluster nodes
3. **Dynamic Scheduler**: Orchestrates scheduling decisions and provides recommendations

## Usage

### Basic Usage

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: dynamic-llama-7b
spec:
  inference:
    preset:
      name: llama-7b
  resource:
    instanceType: "auto"  # Let dynamic scheduler choose optimal instance type
```

### Custom Models

```yaml
apiVersion: kaito.sh/v1beta1
kind: Workspace
metadata:
  name: dynamic-custom-model
spec:
  inference:
    template:
      spec:
        image: my-custom-model:latest
  resource:
    instanceType: "auto"
    count: 2  # Explicit GPU count for custom models
```

## Dynamic Scheduling Features

### 1. Automatic VRAM Calculation

The system automatically calculates GPU memory requirements based on:
- Model parameter count
- Precision (fp16, fp32, int8, int4)
- Sequence length
- Batch size
- Runtime overhead (vLLM, Transformers, etc.)
- Quantization settings

### 2. GPU Capacity Detection

The system detects available GPU capacity from:
- Node labels (e.g., `node.kaito.sh/gpu-memory`)
- Kubernetes resource capacity
- Dynamic Resource Allocation (DRA) information
- Cloud provider SKU information

### 3. Intelligent Scheduling

The scheduler provides:
- Optimal instance type recommendations
- Multi-GPU deployment strategies
- Fallback options when primary choices aren't available
- Cost optimization suggestions

## Implementation Details

### VRAM Calculation Formula

```
Model Memory = (Parameters × Precision) × (1 + Overhead)
KV Cache = Hidden Size × Num Layers × Sequence Length × Batch Size × Precision
Total VRAM = Model Memory + KV Cache + Runtime Overhead
```

### GPU Detection

The system checks for GPU information in this order:
1. Node labels with GPU specifications
2. Kubernetes resource capacity
3. Cloud provider SKU mappings
4. Fallback to hardcoded configurations

### Scheduling Logic

1. Calculate VRAM requirements for the model
2. Detect available GPU capacity in the cluster
3. Find optimal instance types that can satisfy requirements
4. Provide recommendations with confidence levels
5. Generate fallback options if needed

## Benefits

- **Extensibility**: Easy to add new models without hardcoding requirements
- **Cloud Agnostic**: Works with any cloud provider or on-premises cluster
- **Cost Optimization**: Automatically selects most cost-effective instance types
- **Flexibility**: Handles both preset and custom models
- **Reliability**: Provides fallback options and confidence levels

## Future Enhancements

- Machine learning-based VRAM prediction
- Real-time resource monitoring
- Dynamic model quantization recommendations
- Multi-cloud scheduling optimization
