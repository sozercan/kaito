// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuilderSpec defines the desired state of Builder
type BuilderSpec struct {
	// Model specifies the model to be used for building.
	// +kubebuilder:validation:Required
	Model string `json:"model"`
}

// BuilderStatus defines the observed state of Builder
type BuilderStatus struct {
	// Conditions report the current conditions of the builder.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Builder is the Schema for the builders API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=builders,scope=Namespaced,categories=builder
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Model",type="string",JSONPath=".spec.model",description=""
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""
type Builder struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BuilderSpec   `json:"spec,omitempty"`
	Status BuilderStatus `json:"status,omitempty"`
}

// BuilderList contains a list of Builder
// +kubebuilder:object:root=true
type BuilderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Builder `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Builder{}, &BuilderList{})
}
