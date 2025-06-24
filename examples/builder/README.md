# Builder CRD

The Builder CRD is a simple Kubernetes Custom Resource Definition that allows users to specify a model for building purposes.

## Schema

The Builder CRD has the following schema:

```yaml
apiVersion: kaito.sh/v1beta1
kind: Builder
metadata:
  name: builder-name
spec:
  model: "model-name"  # Required: The model to be used for building
```

## Usage

When a Builder resource is created, the controller will:

1. Validate that the model field is specified
2. Log the model name to the controller logs
3. Set the status condition to indicate successful processing

## Example

```yaml
apiVersion: kaito.sh/v1beta1
kind: Builder
metadata:
  name: builder-sample
spec:
  model: huggingface/llama-2-7b-chat-hf
```

When this resource is applied, you will see a log message like:
```
Builder model configured	model=huggingface/llama-2-7b-chat-hf	builder=default/builder-sample
```

## Running the Controller

To run the Builder controller:

```bash
go run ./cmd/builder/main.go
```

For webhook support:
```bash
go run ./cmd/builder/main.go --webhook=true
```

## CRD Installation

The CRD can be installed using:

```bash
kubectl apply -f config/crd/bases/kaito.sh_builders.yaml
```
