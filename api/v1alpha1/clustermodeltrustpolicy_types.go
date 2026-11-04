package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DefaultClusterPolicyName is the default singleton ClusterModelTrustPolicy name.
	DefaultClusterPolicyName = "default"

	// EnforcementModeEnforce blocks non-compliant workloads at admission.
	EnforcementModeEnforce = "Enforce"
	// EnforcementModeAudit records violations and warnings without blocking admission.
	EnforcementModeAudit = "Audit"
)

// SecretKeySelector references a PEM key inside a Kubernetes Secret.
type SecretKeySelector struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Key       string `json:"key"`
}

// TrustedPrincipalSpec defines a trusted public key within a specific trust domain.
type TrustedPrincipalSpec struct {
	ID                       string             `json:"id"`
	Name                     string             `json:"name,omitempty"`
	Role                     string             `json:"role,omitempty"`
	KeyID                    string             `json:"keyID,omitempty"`
	KeyFingerprint           string             `json:"keyFingerprint,omitempty"`
	AllowOperatorEndorsement bool               `json:"allowOperatorEndorsement,omitempty"`
	PublicKeyPEM             string             `json:"publicKeyPEM,omitempty"`
	SecretRef                *SecretKeySelector `json:"secretRef,omitempty"`
}

// TrustDomainsSpec separates cryptographic trust into three isolated domains.
type TrustDomainsSpec struct {
	// ModelSigners are authorized to sign OpenSSF Model Signing (OMS v1.0) artifacts.
	ModelSigners []TrustedPrincipalSpec `json:"modelSigners"`
	// Evaluators are authorized to sign evaluation-evidence/v1 attestations produced by llm-release-gate.
	Evaluators []TrustedPrincipalSpec `json:"evaluators"`
	// ReleaseAuthorities are authorized to sign release-qualification/v1 attestations.
	ReleaseAuthorities []TrustedPrincipalSpec `json:"releaseAuthorities"`
}

// PolicyRequirementsSpec defines the mandatory cryptographic and behavioral checks.
type PolicyRequirementsSpec struct {
	RequireOMSSignature           bool   `json:"requireOMSSignature"`
	RequireCycloneDXBOM           bool   `json:"requireCycloneDXBOM"`
	RequireEvaluationGatePass     bool   `json:"requireEvaluationGatePass"`
	RequireNativeEvaluatorBinding bool   `json:"requireNativeEvaluatorBinding"`
	RejectPostHocBinding          bool   `json:"rejectPostHocBinding"`
	RequireReleaseQualification   bool   `json:"requireReleaseQualification"`
	RequireImmutableRuntimeImage  bool   `json:"requireImmutableRuntimeImage"`
	RequireServingConfigBinding   bool   `json:"requireServingConfigBinding"`
	RequireVerifiedLoRABase       bool   `json:"requireVerifiedLoRABase"`
	AllowUnknownBundleFiles       bool   `json:"allowUnknownBundleFiles,omitempty"`
	ExpectedSuiteDigest           string `json:"expectedSuiteDigest,omitempty"`
	ExpectedDatasetDigest         string `json:"expectedDatasetDigest,omitempty"`
}

// PolicyExemptionsSpec defines explicit break-glass or system exemptions that are hashed into policyDigest.
type PolicyExemptionsSpec struct {
	// ExemptServiceAccounts lists full service account subjects or glob patterns (e.g., "system:serviceaccount:kube-system:*").
	ExemptServiceAccounts []string `json:"exemptServiceAccounts,omitempty"`
	// ExemptPodLabels defines a map of label key/values that exempt system DaemonSets/Pods only when matched.
	ExemptPodLabels map[string]string `json:"exemptPodLabels,omitempty"`
}

// ClusterModelTrustPolicySpec defines the cluster-wide supply-chain trust policy for AI model deployments.
type ClusterModelTrustPolicySpec struct {
	// EnforcementMode is "Enforce" (default) or "Audit".
	EnforcementMode string `json:"enforcementMode,omitempty"`
	// AllowedRegistries restricts OCI Model Artifact URIs to approved registry prefixes.
	AllowedRegistries []string `json:"allowedRegistries"`
	// VerificationTTLSeconds defines how long a ModelArtifact verification remains valid before re-verification is required (default 3600).
	VerificationTTLSeconds int64 `json:"verificationTTLSeconds,omitempty"`
	// MaxEvidenceAgeHours bounds the maximum age of evaluation and release attestations (0 = unlimited).
	MaxEvidenceAgeHours int `json:"maxEvidenceAgeHours,omitempty"`
	// TrustDomains defines the isolated cryptographic key domains for modelSigners, evaluators, and releaseAuthorities.
	TrustDomains TrustDomainsSpec `json:"trustDomains"`
	// Requirements defines the required verification checks.
	Requirements PolicyRequirementsSpec `json:"requirements"`
	// Exemptions defines explicit, auditable exemptions included in the canonical policyDigest.
	Exemptions PolicyExemptionsSpec `json:"exemptions,omitempty"`
}

// ClusterModelTrustPolicyStatus defines the observed status of a ClusterModelTrustPolicy.
type ClusterModelTrustPolicyStatus struct {
	ObservedGeneration                   int64              `json:"observedGeneration,omitempty"`
	Ready                                bool               `json:"ready"`
	PolicyDigest                         string             `json:"policyDigest,omitempty"`
	ResolvedModelSignerFingerprints      []string           `json:"resolvedModelSignerFingerprints,omitempty"`
	ResolvedEvaluatorFingerprints        []string           `json:"resolvedEvaluatorFingerprints,omitempty"`
	ResolvedReleaseAuthorityFingerprints []string           `json:"resolvedReleaseAuthorityFingerprints,omitempty"`
	LastReconciledAt                     *metav1.Time       `json:"lastReconciledAt,omitempty"`
	FailureReason                        string             `json:"failureReason,omitempty"`
	FailureMessage                       string             `json:"failureMessage,omitempty"`
	Conditions                           []metav1.Condition `json:"conditions,omitempty"`
}

// ClusterModelTrustPolicy is the Schema for the clustermodeltrustpolicies API.
type ClusterModelTrustPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterModelTrustPolicySpec   `json:"spec,omitempty"`
	Status ClusterModelTrustPolicyStatus `json:"status,omitempty"`
}

// ClusterModelTrustPolicyList contains a list of ClusterModelTrustPolicy.
type ClusterModelTrustPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterModelTrustPolicy `json:"items"`
}
