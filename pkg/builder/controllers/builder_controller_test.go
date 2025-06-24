// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package controllers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
)

func TestBuilderReconciler_Reconcile(t *testing.T) {
	// Create a scheme with our CRD
	scheme := runtime.NewScheme()
	_ = kaitov1beta1.AddToScheme(scheme)

	// Create a fake client
	builder := &kaitov1beta1.Builder{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-builder",
			Namespace: "default",
		},
		Spec: kaitov1beta1.BuilderSpec{
			Model: "huggingface/llama-2-7b-chat-hf",
		},
	}
	
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(builder).
		Build()

	// Create the reconciler
	reconciler := &BuilderReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// Create a reconcile request
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-builder",
			Namespace: "default",
		},
	}

	// Reconcile
	result, err := reconciler.Reconcile(context.TODO(), req)

	// Assertions
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	// Verify the builder still exists and has the correct model
	var updatedBuilder kaitov1beta1.Builder
	err = fakeClient.Get(context.TODO(), req.NamespacedName, &updatedBuilder)
	assert.NoError(t, err)
	assert.Equal(t, "huggingface/llama-2-7b-chat-hf", updatedBuilder.Spec.Model)
}
