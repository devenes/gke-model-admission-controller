package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Condition types for ModelArtifact.status.conditions.
const (
	ConditionReady                      = "Ready"
	ConditionArtifactResolved           = "ArtifactResolved"
	ConditionBundleIdentityVerified     = "BundleIdentityVerified"
	ConditionOMSSignatureVerified       = "OMSSignatureVerified"
	ConditionBOMVerified                = "BOMVerified"
	ConditionEvaluationVerified         = "EvaluationVerified"
	ConditionReleaseQualificationPassed = "ReleaseQualificationPassed"
)

// Canonical failure reason codes for ModelArtifact and Admission decisions.
const (
	ReasonVerified                      = "Verified"
	ReasonMissingModelArtifactReference = "MissingModelArtifactReference"
	ReasonModelArtifactNotFound         = "ModelArtifactNotFound"
	ReasonVerificationPending           = "VerificationPending"
	ReasonStaleGeneration               = "StaleObservedGeneration"
	ReasonPolicyDigestMismatch          = "PolicyDigestMismatch"
	ReasonVerificationExpired           = "VerificationExpired"
	ReasonUntrustedRegistry             = "UntrustedRegistry"
	ReasonInvalidArtifactURI            = "InvalidArtifactURI"
	ReasonBundleIdentityMismatch        = "BundleIdentityMismatch"
	ReasonInvalidBundleManifest         = "InvalidBundleManifest"
	ReasonOMSSignatureInvalid           = "OMSSignatureInvalid"
	ReasonUntrustedModelSigner          = "UntrustedModelSigner"
	ReasonBOMVerificationFailed         = "BOMVerificationFailed"
	ReasonEvaluationGateFailed          = "EvaluationGateFailed"
	ReasonPostHocEvaluationRejected     = "PostHocEvaluationRejected"
	ReasonUntrustedEvaluator            = "UntrustedEvaluator"
	ReasonReleaseQualificationFailed    = "ReleaseQualificationFailed"
	ReasonUntrustedReleaseAuthority     = "UntrustedReleaseAuthority"
	ReasonCrossEvidenceMismatch         = "CrossEvidenceMismatch"
	ReasonRuntimeImageMismatch          = "RuntimeImageMismatch"
	ReasonMutableRuntimeImage           = "MutableRuntimeImage"
	ReasonServingConfigDigestMismatch   = "ServingConfigDigestMismatch"
	ReasonLoRABaseModelUnverified       = "LoRABaseModelUnverified"
	ReasonClusterPolicyNotReady         = "ClusterPolicyNotReady"
)

// ArtifactSourceSpec defines the immutable OCI Model Artifact coordinate.
type ArtifactSourceSpec struct {
	// URI is the digest-pinned OCI artifact reference, e.g.,
	// "us-west4-docker.pkg.dev/PROJECT_ID/models/gemma-2b-it@sha256:<64-hex>".
	URI string `json:"uri"`
}

// BundleIdentitySpec declares the expected deterministic bundle-identity/v1 digest.
type BundleIdentitySpec struct {
	// BundleID is the expected "sha256:<64-hex>" bundle-identity/v1 digest.
	BundleID string `json:"bundleID"`
}

// RuntimeBindingSpec defines optional runtime container image, serving configuration, and LoRA base bindings.
type RuntimeBindingSpec struct {
	// RuntimeImage is the expected immutable digest-pinned serving container image (`@sha256:...`).
	RuntimeImage string `json:"runtimeImage,omitempty"`
	// ServingConfigDigest is the expected "sha256:<64-hex>" digest of the serving configuration.
	ServingConfigDigest string `json:"servingConfigDigest,omitempty"`
	// BaseModelArtifact is the name of the verified base ModelArtifact in the same namespace when this artifact is a LoRA adapter.
	BaseModelArtifact string `json:"baseModelArtifact,omitempty"`
}

// EvidenceRefsSpec optionally pins specific referrer manifest digests when multiple referrers exist.
type EvidenceRefsSpec struct {
	OMSSignatureDigest          string `json:"omsSignatureDigest,omitempty"`
	BOMArtifactDigest           string `json:"bomArtifactDigest,omitempty"`
	EvaluationAttestationDigest string `json:"evaluationAttestationDigest,omitempty"`
	ReleaseQualificationDigest  string `json:"releaseQualificationDigest,omitempty"`
}

// ModelArtifactSpec defines the desired, immutable cryptographic identity of an AI model artifact.
type ModelArtifactSpec struct {
	// PolicyName optionally selects a specific ClusterModelTrustPolicy (defaults to "default").
	PolicyName string `json:"policyName,omitempty"`
	// Artifact specifies the immutable OCI Model Artifact URI.
	Artifact ArtifactSourceSpec `json:"artifact"`
	// Identity specifies the expected deterministic bundle-identity/v1 digest.
	Identity BundleIdentitySpec `json:"identity"`
	// Runtime specifies optional runtime image, serving config, and LoRA base model bindings.
	Runtime RuntimeBindingSpec `json:"runtime,omitempty"`
	// EvidenceRefs optionally pins specific OCI referrer digests.
	EvidenceRefs EvidenceRefsSpec `json:"evidenceRefs,omitempty"`
}

// ModelArtifactStatus defines the observed cryptographic verification state of a ModelArtifact.
type ModelArtifactStatus struct {
	ObservedGeneration          int64              `json:"observedGeneration,omitempty"`
	Verified                    bool               `json:"verified"`
	PolicyName                  string             `json:"policyName,omitempty"`
	PolicyDigest                string             `json:"policyDigest,omitempty"`
	ResolvedManifestDigest      string             `json:"resolvedManifestDigest,omitempty"`
	BundleManifestDigest        string             `json:"bundleManifestDigest,omitempty"`
	VerifiedBundleID            string             `json:"verifiedBundleID,omitempty"`
	BOMCanonicalDigest          string             `json:"bomCanonicalDigest,omitempty"`
	OMSSignatureDigest          string             `json:"omsSignatureDigest,omitempty"`
	OMSSignerFingerprint        string             `json:"omsSignerFingerprint,omitempty"`
	EvaluationReportDigest      string             `json:"evaluationReportDigest,omitempty"`
	EvaluationAttestationDigest string             `json:"evaluationAttestationDigest,omitempty"`
	EvaluationGateStatus        string             `json:"evaluationGateStatus,omitempty"`
	EvaluationBindingMode       string             `json:"evaluationBindingMode,omitempty"`
	EvaluationSignerFingerprint string             `json:"evaluationSignerFingerprint,omitempty"`
	ReleaseQualificationDigest  string             `json:"releaseQualificationDigest,omitempty"`
	ReleaseAuthorityFingerprint string             `json:"releaseAuthorityFingerprint,omitempty"`
	RuntimeImage                string             `json:"runtimeImage,omitempty"`
	ServingConfigDigest         string             `json:"servingConfigDigest,omitempty"`
	LoRAVerificationStatus      string             `json:"loraVerificationStatus,omitempty"`
	VerifiedBaseBundleID        string             `json:"verifiedBaseBundleID,omitempty"`
	LastVerifiedAt              *metav1.Time       `json:"lastVerifiedAt,omitempty"`
	ValidUntil                  *metav1.Time       `json:"validUntil,omitempty"`
	FailureReason               string             `json:"failureReason,omitempty"`
	FailureMessage              string             `json:"failureMessage,omitempty"`
	Conditions                  []metav1.Condition `json:"conditions,omitempty"`
}

// ModelArtifact is the Schema for the modelartifacts API.
type ModelArtifact struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModelArtifactSpec   `json:"spec,omitempty"`
	Status ModelArtifactStatus `json:"status,omitempty"`
}

// ModelArtifactList contains a list of ModelArtifact.
type ModelArtifactList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModelArtifact `json:"items"`
}
