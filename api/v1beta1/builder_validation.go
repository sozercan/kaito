// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package v1beta1

import (
	"context"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"knative.dev/pkg/apis"
)

func (b *Builder) SupportedVerbs() []admissionregistrationv1.OperationType {
	return []admissionregistrationv1.OperationType{
		admissionregistrationv1.Create,
		admissionregistrationv1.Update,
	}
}

func (b *Builder) Validate(ctx context.Context) (errs *apis.FieldError) {
	base := apis.GetBaseline(ctx)
	if base == nil {
		errs = errs.Also(b.validateCreate())
	} else {
		errs = errs.Also(b.validateUpdate(base.(*Builder)))
	}
	return errs
}

func (b *Builder) validateCreate() (errs *apis.FieldError) {
	// Validate that model is not empty
	if b.Spec.Model == "" {
		errs = errs.Also(apis.ErrMissingField("spec.model"))
	}
	return errs
}

func (b *Builder) validateUpdate(old *Builder) (errs *apis.FieldError) {
	// Currently allow all updates
	return b.validateCreate()
}
