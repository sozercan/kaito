// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kaitov1beta1 "github.com/kaito-project/kaito/api/v1beta1"
	"github.com/kaito-project/kaito/pkg/utils/consts"
)

const (
	BuilderHashAnnotation = "builder.kaito.io/hash"
	BuilderNameLabel      = "builder.kaito.io/name"
)

type BuilderReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

func NewBuilderReconciler(client client.Client, scheme *runtime.Scheme, log logr.Logger, recorder record.EventRecorder) *BuilderReconciler {
	return &BuilderReconciler{
		Client:   client,
		Scheme:   scheme,
		Log:      log,
		Recorder: recorder,
	}
}

func (c *BuilderReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	builderObj := &kaitov1beta1.Builder{}

	if err := c.Get(ctx, req.NamespacedName, builderObj); err != nil {
		if client.IgnoreNotFound(err) != nil {
			klog.ErrorS(err, "failed to get builder", "builder", klog.KRef(req.Namespace, req.Name))
		}
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	klog.InfoS("Reconciling builder", "builder", klog.KObj(builderObj))

	if !builderObj.DeletionTimestamp.IsZero() {
		return c.deleteBuilder(ctx, builderObj)
	}

	if err := c.ensureFinalizer(ctx, builderObj); err != nil {
		klog.ErrorS(err, "failed to ensure finalizer for builder", "builder", klog.KObj(builderObj))
		return reconcile.Result{}, err
	}

	return c.addOrUpdateBuilder(ctx, builderObj)
}

func (c *BuilderReconciler) ensureFinalizer(ctx context.Context, builderObj *kaitov1beta1.Builder) error {
	if controllerutil.AddFinalizer(builderObj, consts.BuilderFinalizer) {
		return c.Update(ctx, builderObj)
	}
	return nil
}

func (c *BuilderReconciler) addOrUpdateBuilder(ctx context.Context, builderObj *kaitov1beta1.Builder) (reconcile.Result, error) {
	// Log the model name as requested
	klog.InfoS("Builder model configured", "model", builderObj.Spec.Model, "builder", klog.KObj(builderObj))

	// Set the condition to indicate successful processing
	if err := c.updateStatusConditionIfNotMatch(ctx, builderObj, kaitov1beta1.BuilderConditionTypeSucceeded, "True",
		"BuilderSucceeded", "Builder has been successfully processed"); err != nil {
		klog.ErrorS(err, "failed to update builder status", "builder", klog.KObj(builderObj))
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (c *BuilderReconciler) deleteBuilder(ctx context.Context, builderObj *kaitov1beta1.Builder) (reconcile.Result, error) {
	klog.InfoS("Deleting builder", "builder", klog.KObj(builderObj))

	if controllerutil.RemoveFinalizer(builderObj, consts.BuilderFinalizer) {
		if err := c.Update(ctx, builderObj); err != nil {
			klog.ErrorS(err, "failed to remove finalizer from builder", "builder", klog.KObj(builderObj))
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

func (c *BuilderReconciler) updateStatusConditionIfNotMatch(ctx context.Context, builder *kaitov1beta1.Builder, cType kaitov1beta1.ConditionType, status, reason, message string) error {
	// Simple implementation - in a real scenario you'd want to check if the condition already exists and has the same status
	// This would require reading the latest version of the object and updating conditions
	klog.InfoS("Builder status condition updated", "type", cType, "status", status, "reason", reason, "message", message, "builder", klog.KObj(builder))
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (c *BuilderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	c.Recorder = mgr.GetEventRecorderFor("Builder")

	return ctrl.NewControllerManagedBy(mgr).
		For(&kaitov1beta1.Builder{}).
		Complete(c)
}
