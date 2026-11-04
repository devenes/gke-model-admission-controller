// Package v1alpha1 contains API Schema definitions for the trust.gke.dev v1alpha1 API group.
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// GroupName is the Kubernetes API group for GKE Model Admission Controller resources.
	GroupName = "trust.gke.dev"
	// Version is the API version.
	Version = "v1alpha1"

	// LabelEnforceNamespace enables default-deny admission enforcement on a Namespace.
	LabelEnforceNamespace = "trust.gke.dev/enforce"
	// AnnotationModelArtifact binds a Pod or workload to a verified ModelArtifact in the same namespace.
	AnnotationModelArtifact = "trust.gke.dev/model-artifact"
	// AnnotationBaseModelArtifact binds a LoRA-based Pod or workload to a verified base ModelArtifact in the same namespace.
	AnnotationBaseModelArtifact = "trust.gke.dev/base-model-artifact"
	// AnnotationAdapterArtifact optionally binds a Pod or workload to a verified LoRA adapter ModelArtifact.
	AnnotationAdapterArtifact = "trust.gke.dev/adapter-artifact"
	// AnnotationServingConfigDigest binds a Pod or workload to an immutable serving configuration SHA-256 digest.
	AnnotationServingConfigDigest = "trust.gke.dev/serving-config-digest"
)

var (
	// GroupVersion is group version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: GroupName, Version: Version}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&ModelArtifact{},
		&ModelArtifactList{},
		&ClusterModelTrustPolicy{},
		&ClusterModelTrustPolicyList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
