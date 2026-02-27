package controllers

import (
	"context"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	featureflagsv1alpha1 "github.com/example/featureflags-operator/api/v1alpha1"
)

// FeatureFlagReconciler reconciles FeatureFlag objects.
type FeatureFlagReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// Reconcile is the main reconciliation loop.
func (r *FeatureFlagReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := r.Log.WithValues("featureflag", req.NamespacedName)

	// 1. Fetch the FeatureFlag.
	var ff featureflagsv1alpha1.FeatureFlag
	if err := r.Get(ctx, req.NamespacedName, &ff); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		log.Error(err, "unable to fetch FeatureFlag")
		return reconcile.Result{}, err
	}

	// 2. Fetch the referenced ConfigMap.
	cmRef := ff.Spec.ConfigMapRef
	cmKey := types.NamespacedName{Name: cmRef.Name, Namespace: cmRef.Namespace}

	value := ff.Spec.DefaultValue
	readyStatus := metav1.ConditionTrue
	readyReason := "ConfigMapFound"
	readyMessage := "Successfully read value from ConfigMap"

	var cm corev1.ConfigMap
	if err := r.Get(ctx, cmKey, &cm); err != nil {
		if errors.IsNotFound(err) {
			readyStatus = metav1.ConditionFalse
			readyReason = "ConfigMapNotFound"
			readyMessage = "ConfigMap " + cmKey.String() + " not found; using defaultValue"
			log.Info("ConfigMap not found, using defaultValue", "configmap", cmKey)
		} else {
			log.Error(err, "unable to fetch ConfigMap", "configmap", cmKey)
			return reconcile.Result{}, err
		}
	} else {
		// 3. Read the key.
		if v, ok := cm.Data[cmRef.Key]; ok {
			value = v
		} else {
			readyStatus = metav1.ConditionFalse
			readyReason = "KeyNotFound"
			readyMessage = "Key " + cmRef.Key + " not found in ConfigMap; using defaultValue"
			log.Info("Key not found in ConfigMap, using defaultValue", "key", cmRef.Key, "configmap", cmKey)
		}
	}

	// 4. Determine enabled state.
	enabled := isTruthy(value)

	// 5. Patch Deployment args if deploymentRef is set.
	if ref := ff.Spec.DeploymentRef; ref != nil {
		if err := r.patchDeploymentArg(ctx, log, ref, value); err != nil {
			return reconcile.Result{}, err
		}
	}

	// 6. Build and write updated status.
	now := time.Now().UTC()
	newCondition := metav1.Condition{
		Type:               "Ready",
		Status:             readyStatus,
		Reason:             readyReason,
		Message:            readyMessage,
		LastTransitionTime: metav1.NewTime(now),
		ObservedGeneration: ff.Generation,
	}
	// Preserve LastTransitionTime when condition status hasn't changed.
	for _, existing := range ff.Status.Conditions {
		if existing.Type == "Ready" && existing.Status == readyStatus {
			newCondition.LastTransitionTime = existing.LastTransitionTime
			break
		}
	}

	ff.Status = featureflagsv1alpha1.FeatureFlagStatus{
		Value:       value,
		Enabled:     enabled,
		LastUpdated: now.Format(time.RFC3339),
		Conditions:  []metav1.Condition{newCondition},
	}

	if err := r.Status().Update(ctx, &ff); err != nil {
		if errors.IsConflict(err) {
			log.V(1).Info("conflict updating status, requeueing")
			return reconcile.Result{Requeue: true}, nil
		}
		log.Error(err, "unable to update FeatureFlag status")
		return reconcile.Result{}, err
	}

	log.Info("reconciled FeatureFlag", "value", value, "enabled", enabled)
	return reconcile.Result{}, nil
}

// patchDeploymentArg finds the named container in the Deployment and sets
// or replaces the arg "<argName>=<value>".
func (r *FeatureFlagReconciler) patchDeploymentArg(
	ctx context.Context,
	log logr.Logger,
	ref *featureflagsv1alpha1.DeploymentRef,
	value string,
) error {
	depKey := types.NamespacedName{Name: ref.Name, Namespace: ref.Namespace}
	var dep appsv1.Deployment
	if err := r.Get(ctx, depKey, &dep); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Deployment not found, skipping arg patch", "deployment", depKey)
			return nil
		}
		log.Error(err, "unable to fetch Deployment", "deployment", depKey)
		return err
	}

	containers := dep.Spec.Template.Spec.Containers
	containerIdx := -1
	for i := range containers {
		if containers[i].Name == ref.Container {
			containerIdx = i
			break
		}
	}
	if containerIdx < 0 {
		log.Info("container not found in Deployment, skipping arg patch",
			"deployment", depKey, "container", ref.Container)
		return nil
	}

	newArg := ref.ArgName + "=" + value
	prefix := ref.ArgName + "="
	args := containers[containerIdx].Args
	replaced := false
	for i, arg := range args {
		if strings.HasPrefix(arg, prefix) || arg == ref.ArgName {
			args[i] = newArg
			replaced = true
			break
		}
	}
	if !replaced {
		dep.Spec.Template.Spec.Containers[containerIdx].Args = append(args, newArg)
	} else {
		dep.Spec.Template.Spec.Containers[containerIdx].Args = args
	}

	if err := r.Update(ctx, &dep); err != nil {
		if errors.IsConflict(err) {
			log.V(1).Info("conflict updating Deployment, requeueing")
			return err
		}
		log.Error(err, "unable to update Deployment", "deployment", depKey)
		return err
	}
	log.Info("patched Deployment arg", "deployment", depKey, "arg", newArg)
	return nil
}

// isTruthy returns true for common truthy string values.
func isTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

// SetupWithManager registers the reconciler and sets up the secondary ConfigMap watch.
func (r *FeatureFlagReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// configMapToFeatureFlags maps a ConfigMap event to reconcile.Requests for
	// all FeatureFlag objects that reference that ConfigMap.
	configMapToFeatureFlags := func(ctx context.Context, obj client.Object) []reconcile.Request {
		cm, ok := obj.(*corev1.ConfigMap)
		if !ok {
			return nil
		}

		var flagList featureflagsv1alpha1.FeatureFlagList
		if err := mgr.GetClient().List(ctx, &flagList); err != nil {
			mgr.GetLogger().Error(err, "unable to list FeatureFlags for ConfigMap mapping",
				"configmap", types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace})
			return nil
		}

		var requests []reconcile.Request
		for _, ff := range flagList.Items {
			ref := ff.Spec.ConfigMapRef
			if ref.Name == cm.Name && ref.Namespace == cm.Namespace {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      ff.Name,
						Namespace: ff.Namespace,
					},
				})
			}
		}
		return requests
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&featureflagsv1alpha1.FeatureFlag{}).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(configMapToFeatureFlags),
		).
		Complete(r)
}
