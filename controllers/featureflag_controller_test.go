package controllers

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	featureflagsv1alpha1 "github.com/example/featureflags-operator/api/v1alpha1"
)

// ---- helpers ----------------------------------------------------------------

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := featureflagsv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func newTestReconciler(t *testing.T, objs ...client.Object) (*FeatureFlagReconciler, client.Client) {
	t.Helper()
	s := newTestScheme(t)
	c := fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objs...).
		WithStatusSubresource(&featureflagsv1alpha1.FeatureFlag{}).
		Build()
	r := &FeatureFlagReconciler{
		Client: c,
		Log:    logr.Discard(),
		Scheme: s,
	}
	return r, c
}

func req(name, namespace string) reconcile.Request {
	return reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: namespace}}
}

func getFF(t *testing.T, c client.Client, name, namespace string) featureflagsv1alpha1.FeatureFlag {
	t.Helper()
	var ff featureflagsv1alpha1.FeatureFlag
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, &ff); err != nil {
		t.Fatalf("get FeatureFlag: %v", err)
	}
	return ff
}

func getDep(t *testing.T, c client.Client, name, namespace string) appsv1.Deployment {
	t.Helper()
	var dep appsv1.Deployment
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, &dep); err != nil {
		t.Fatalf("get Deployment: %v", err)
	}
	return dep
}

// minimalDeployment builds a Deployment with a single container and optional args.
func minimalDeployment(name, namespace, container string, args ...string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: container, Image: "nginx", Args: args},
					},
				},
			},
		},
	}
}

// ---- isTruthy ---------------------------------------------------------------

func TestIsTruthy(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"1", true},
		{"yes", true},
		{"YES", true},
		{"on", true},
		{"ON", true},
		{"  true  ", true}, // whitespace trimmed
		{"false", false},
		{"0", false},
		{"no", false},
		{"off", false},
		{"", false},
		{"enabled", false},
		{"t", false},
		{"2", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isTruthy(tt.input); got != tt.want {
				t.Errorf("isTruthy(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ---- Reconcile: FeatureFlag not found ---------------------------------------

func TestReconcile_FeatureFlagNotFound(t *testing.T) {
	r, _ := newTestReconciler(t)
	result, err := r.Reconcile(context.Background(), req("nonexistent", "default"))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result.Requeue {
		t.Error("expected Requeue=false when FeatureFlag is absent")
	}
}

// ---- Reconcile: ConfigMap found, truthy value -------------------------------

func TestReconcile_ConfigMapFound_TruthyValue(t *testing.T) {
	ff := &featureflagsv1alpha1.FeatureFlag{
		ObjectMeta: metav1.ObjectMeta{Name: "ff", Namespace: "default"},
		Spec: featureflagsv1alpha1.FeatureFlagSpec{
			ConfigMapRef: featureflagsv1alpha1.ConfigMapKeyRef{
				Name: "cm", Namespace: "default", Key: "flag",
			},
			DefaultValue: "false",
		},
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "default"},
		Data:       map[string]string{"flag": "true"},
	}

	r, c := newTestReconciler(t, ff, cm)
	if _, err := r.Reconcile(context.Background(), req("ff", "default")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := getFF(t, c, "ff", "default")

	if got.Status.Value != "true" {
		t.Errorf("Status.Value = %q, want %q", got.Status.Value, "true")
	}
	if !got.Status.Enabled {
		t.Error("Status.Enabled = false, want true")
	}
	if got.Status.LastUpdated == "" {
		t.Error("Status.LastUpdated is empty")
	}
	if len(got.Status.Conditions) == 0 {
		t.Fatal("no conditions set")
	}
	cond := got.Status.Conditions[0]
	if cond.Type != "Ready" {
		t.Errorf("condition Type = %q, want Ready", cond.Type)
	}
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("condition Status = %q, want True", cond.Status)
	}
	if cond.Reason != "ConfigMapFound" {
		t.Errorf("condition Reason = %q, want ConfigMapFound", cond.Reason)
	}
}

// ---- Reconcile: ConfigMap found, falsy value --------------------------------

func TestReconcile_ConfigMapFound_FalsyValue(t *testing.T) {
	ff := &featureflagsv1alpha1.FeatureFlag{
		ObjectMeta: metav1.ObjectMeta{Name: "ff", Namespace: "default"},
		Spec: featureflagsv1alpha1.FeatureFlagSpec{
			ConfigMapRef: featureflagsv1alpha1.ConfigMapKeyRef{
				Name: "cm", Namespace: "default", Key: "flag",
			},
		},
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "default"},
		Data:       map[string]string{"flag": "false"},
	}

	r, c := newTestReconciler(t, ff, cm)
	if _, err := r.Reconcile(context.Background(), req("ff", "default")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := getFF(t, c, "ff", "default")
	if got.Status.Value != "false" {
		t.Errorf("Status.Value = %q, want false", got.Status.Value)
	}
	if got.Status.Enabled {
		t.Error("Status.Enabled = true, want false")
	}
}

// ---- Reconcile: ConfigMap missing, falls back to defaultValue ---------------

func TestReconcile_ConfigMapNotFound(t *testing.T) {
	ff := &featureflagsv1alpha1.FeatureFlag{
		ObjectMeta: metav1.ObjectMeta{Name: "ff", Namespace: "default"},
		Spec: featureflagsv1alpha1.FeatureFlagSpec{
			ConfigMapRef: featureflagsv1alpha1.ConfigMapKeyRef{
				Name: "missing", Namespace: "default", Key: "flag",
			},
			DefaultValue: "yes",
		},
	}

	r, c := newTestReconciler(t, ff)
	if _, err := r.Reconcile(context.Background(), req("ff", "default")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := getFF(t, c, "ff", "default")
	if got.Status.Value != "yes" {
		t.Errorf("Status.Value = %q, want yes (defaultValue)", got.Status.Value)
	}
	if !got.Status.Enabled {
		t.Error("Status.Enabled = false, want true ('yes' is truthy)")
	}
	if len(got.Status.Conditions) == 0 {
		t.Fatal("no conditions set")
	}
	cond := got.Status.Conditions[0]
	if cond.Status != metav1.ConditionFalse {
		t.Errorf("condition Status = %q, want False", cond.Status)
	}
	if cond.Reason != "ConfigMapNotFound" {
		t.Errorf("condition Reason = %q, want ConfigMapNotFound", cond.Reason)
	}
}

// ---- Reconcile: key missing inside ConfigMap --------------------------------

func TestReconcile_KeyNotFound(t *testing.T) {
	ff := &featureflagsv1alpha1.FeatureFlag{
		ObjectMeta: metav1.ObjectMeta{Name: "ff", Namespace: "default"},
		Spec: featureflagsv1alpha1.FeatureFlagSpec{
			ConfigMapRef: featureflagsv1alpha1.ConfigMapKeyRef{
				Name: "cm", Namespace: "default", Key: "missing-key",
			},
			DefaultValue: "on",
		},
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "default"},
		Data:       map[string]string{"other": "value"},
	}

	r, c := newTestReconciler(t, ff, cm)
	if _, err := r.Reconcile(context.Background(), req("ff", "default")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := getFF(t, c, "ff", "default")
	if got.Status.Value != "on" {
		t.Errorf("Status.Value = %q, want on (defaultValue)", got.Status.Value)
	}
	if !got.Status.Enabled {
		t.Error("Status.Enabled = false, want true ('on' is truthy)")
	}
	if got.Status.Conditions[0].Reason != "KeyNotFound" {
		t.Errorf("condition Reason = %q, want KeyNotFound", got.Status.Conditions[0].Reason)
	}
}

// ---- Reconcile: DeploymentRef — arg appended --------------------------------

func TestReconcile_DeploymentArg_Appended(t *testing.T) {
	ff := &featureflagsv1alpha1.FeatureFlag{
		ObjectMeta: metav1.ObjectMeta{Name: "ff", Namespace: "default"},
		Spec: featureflagsv1alpha1.FeatureFlagSpec{
			ConfigMapRef: featureflagsv1alpha1.ConfigMapKeyRef{
				Name: "cm", Namespace: "default", Key: "flag",
			},
			DeploymentRef: &featureflagsv1alpha1.DeploymentRef{
				Name: "dep", Namespace: "default", Container: "app", ArgName: "--feature",
			},
		},
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "default"},
		Data:       map[string]string{"flag": "true"},
	}
	dep := minimalDeployment("dep", "default", "app")

	r, c := newTestReconciler(t, ff, cm, dep)
	if _, err := r.Reconcile(context.Background(), req("ff", "default")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := getDep(t, c, "dep", "default")
	args := got.Spec.Template.Spec.Containers[0].Args
	if len(args) != 1 || args[0] != "--feature=true" {
		t.Errorf("container args = %v, want [--feature=true]", args)
	}
}

// ---- Reconcile: DeploymentRef — existing arg replaced -----------------------

func TestReconcile_DeploymentArg_Replaced(t *testing.T) {
	ff := &featureflagsv1alpha1.FeatureFlag{
		ObjectMeta: metav1.ObjectMeta{Name: "ff", Namespace: "default"},
		Spec: featureflagsv1alpha1.FeatureFlagSpec{
			ConfigMapRef: featureflagsv1alpha1.ConfigMapKeyRef{
				Name: "cm", Namespace: "default", Key: "flag",
			},
			DeploymentRef: &featureflagsv1alpha1.DeploymentRef{
				Name: "dep", Namespace: "default", Container: "app", ArgName: "--feature",
			},
		},
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "default"},
		Data:       map[string]string{"flag": "false"},
	}
	// Deployment already has the arg set to a previous value plus an unrelated arg.
	dep := minimalDeployment("dep", "default", "app", "--other=keep", "--feature=true")

	r, c := newTestReconciler(t, ff, cm, dep)
	if _, err := r.Reconcile(context.Background(), req("ff", "default")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := getDep(t, c, "dep", "default")
	args := got.Spec.Template.Spec.Containers[0].Args
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %v", args)
	}
	if args[0] != "--other=keep" {
		t.Errorf("args[0] = %q, want --other=keep (unchanged)", args[0])
	}
	if args[1] != "--feature=false" {
		t.Errorf("args[1] = %q, want --feature=false (replaced)", args[1])
	}
}

// ---- Reconcile: DeploymentRef — Deployment does not exist ------------------

func TestReconcile_DeploymentNotFound(t *testing.T) {
	ff := &featureflagsv1alpha1.FeatureFlag{
		ObjectMeta: metav1.ObjectMeta{Name: "ff", Namespace: "default"},
		Spec: featureflagsv1alpha1.FeatureFlagSpec{
			ConfigMapRef: featureflagsv1alpha1.ConfigMapKeyRef{
				Name: "cm", Namespace: "default", Key: "flag",
			},
			DeploymentRef: &featureflagsv1alpha1.DeploymentRef{
				Name: "nonexistent", Namespace: "default", Container: "app", ArgName: "--feature",
			},
		},
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "default"},
		Data:       map[string]string{"flag": "true"},
	}

	r, c := newTestReconciler(t, ff, cm)
	if _, err := r.Reconcile(context.Background(), req("ff", "default")); err != nil {
		t.Fatalf("missing Deployment should not cause an error, got %v", err)
	}

	// Status should still be updated despite the missing Deployment.
	got := getFF(t, c, "ff", "default")
	if got.Status.Value != "true" {
		t.Errorf("Status.Value = %q, want true", got.Status.Value)
	}
}

// ---- Reconcile: DeploymentRef — container not found in pod spec -------------

func TestReconcile_ContainerNotFound(t *testing.T) {
	ff := &featureflagsv1alpha1.FeatureFlag{
		ObjectMeta: metav1.ObjectMeta{Name: "ff", Namespace: "default"},
		Spec: featureflagsv1alpha1.FeatureFlagSpec{
			ConfigMapRef: featureflagsv1alpha1.ConfigMapKeyRef{
				Name: "cm", Namespace: "default", Key: "flag",
			},
			DeploymentRef: &featureflagsv1alpha1.DeploymentRef{
				Name: "dep", Namespace: "default", Container: "no-such-container", ArgName: "--feature",
			},
		},
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "default"},
		Data:       map[string]string{"flag": "true"},
	}
	dep := minimalDeployment("dep", "default", "app")

	r, c := newTestReconciler(t, ff, cm, dep)
	if _, err := r.Reconcile(context.Background(), req("ff", "default")); err != nil {
		t.Fatalf("missing container should not cause an error, got %v", err)
	}

	// Deployment args must be untouched.
	got := getDep(t, c, "dep", "default")
	if args := got.Spec.Template.Spec.Containers[0].Args; len(args) != 0 {
		t.Errorf("expected no args on Deployment, got %v", args)
	}
}
