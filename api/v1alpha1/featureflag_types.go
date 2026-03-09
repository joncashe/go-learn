// Package v1alpha1 contains API Schema definitions for the featureflags v1alpha1 API group.
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion is the group version used to register these objects.
var GroupVersion = schema.GroupVersion{Group: "featureflags.example.com", Version: "v1alpha1"}

// ConfigMapKeyRef identifies a key in a ConfigMap.
type ConfigMapKeyRef struct {
	// Name is the name of the ConfigMap.
	Name string `json:"name"`
	// Namespace is the namespace of the ConfigMap.
	Namespace string `json:"namespace"`
	// Key is the key within the ConfigMap's data.
	Key string `json:"key"`
}

// DeploymentRef identifies a Deployment container whose args should be patched.
type DeploymentRef struct {
	// Name is the name of the Deployment.
	Name string `json:"name"`
	// Namespace is the namespace of the Deployment.
	Namespace string `json:"namespace"`
	// Container is the name of the container within the Deployment's pod spec.
	Container string `json:"container"`
	// ArgName is the argument flag to inject, e.g. "--feature-enabled".
	// The reconciler sets or replaces the arg as "<argName>=<value>".
	ArgName string `json:"argName"`
}

// FeatureFlagSpec defines the desired state of FeatureFlag.
type FeatureFlagSpec struct {
	// ConfigMapRef identifies the ConfigMap and key to read the flag value from.
	ConfigMapRef ConfigMapKeyRef `json:"configMapRef"`
	// DefaultValue is used when the ConfigMap or key does not exist.
	// +optional
	DefaultValue string `json:"defaultValue,omitempty"`
	// DeploymentRef optionally identifies a Deployment container whose args
	// should be patched whenever the flag value changes.
	// +optional
	DeploymentRef *DeploymentRef `json:"deploymentRef,omitempty"`
}

// FeatureFlagStatus defines the observed state of FeatureFlag.
type FeatureFlagStatus struct {
	// Value is the raw string value read from the ConfigMap key, or DefaultValue.
	Value string `json:"value,omitempty"`
	// Enabled is true when Value parses as a "truthy" string (true/1/yes/on).
	Enabled bool `json:"enabled"`
	// LastUpdated is the RFC3339 timestamp of the last successful reconciliation.
	LastUpdated string `json:"lastUpdated,omitempty"`
	// Conditions represent the latest available observations of the FeatureFlag's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// FeatureFlag is the Schema for the featureflags API.
type FeatureFlag struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FeatureFlagSpec   `json:"spec,omitempty"`
	Status FeatureFlagStatus `json:"status,omitempty"`
}

// DeepCopyInto copies all properties of this object into another object of the same type.
func (in *FeatureFlag) DeepCopyInto(out *FeatureFlag) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = FeatureFlagSpec{
		ConfigMapRef: in.Spec.ConfigMapRef,
		DefaultValue: in.Spec.DefaultValue,
	}
	if in.Spec.DeploymentRef != nil {
		dr := *in.Spec.DeploymentRef
		out.Spec.DeploymentRef = &dr
	}
	out.Status.Value = in.Status.Value
	out.Status.Enabled = in.Status.Enabled
	out.Status.LastUpdated = in.Status.LastUpdated
	if in.Status.Conditions != nil {
		out.Status.Conditions = make([]metav1.Condition, len(in.Status.Conditions))
		copy(out.Status.Conditions, in.Status.Conditions)
	}
}

// DeepCopy returns a deep copy of the FeatureFlag.
func (in *FeatureFlag) DeepCopy() *FeatureFlag {
	if in == nil {
		return nil
	}
	out := new(FeatureFlag)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *FeatureFlag) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

// FeatureFlagList contains a list of FeatureFlag.
type FeatureFlagList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FeatureFlag `json:"items"`
}

// DeepCopyInto copies all properties of this object into another FeatureFlagList.
func (in *FeatureFlagList) DeepCopyInto(out *FeatureFlagList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]FeatureFlag, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy returns a deep copy of the FeatureFlagList.
func (in *FeatureFlagList) DeepCopy() *FeatureFlagList {
	if in == nil {
		return nil
	}
	out := new(FeatureFlagList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *FeatureFlagList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}
