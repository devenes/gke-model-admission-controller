// Package verification orchestrates OCI v1.1 descriptor resolution, 3-domain digest bridging,
// and in-memory cryptographic verification via ai-model-provenance-gate/pkg/verify.
package verification

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/devenes/ai-model-provenance-gate/pkg/verify"
	"github.com/devenes/gke-model-admission-controller/api/v1alpha1"
	"github.com/devenes/gke-model-admission-controller/internal/policy"
	"github.com/devenes/gke-model-admission-controller/internal/registry"
)

// Engine verifies ModelArtifact specifications against an OCI v1.1 registry and a ResolvedPolicy.
type Engine struct {
	RegistryClient *registry.Client
	NowFunc        func() time.Time
}

// NewEngine creates a new verification Engine.
func NewEngine(regClient *registry.Client) *Engine {
	if regClient == nil {
		regClient = registry.NewClient(nil)
	}
	return &Engine{
		RegistryClient: regClient,
		NowFunc:        func() time.Time { return time.Now().UTC() },
	}
}

// VerifyOptions provides additional context (such as verified base bundle ID for LoRA adapters) during reconciliation.
type VerifyOptions struct {
	ExpectedBaseBundleID string
}

// VerifyModelArtifact executes the 7-stage supply-chain verification pipeline without downloading model weight blobs
// and returns the populated ModelArtifactStatus.
func (e *Engine) VerifyModelArtifact(
	ctx context.Context,
	artifact *v1alpha1.ModelArtifact,
	pol *policy.ResolvedPolicy,
	opts VerifyOptions,
) v1alpha1.ModelArtifactStatus {
	now := time.Now().UTC()
	if e.NowFunc != nil {
		now = e.NowFunc().UTC()
	}
	nowMeta := metav1.NewTime(now)

	status := v1alpha1.ModelArtifactStatus{
		ObservedGeneration: artifact.Generation,
		Verified:           false,
	}
	if pol == nil {
		return failStatus(status, nowMeta, v1alpha1.ConditionReady, v1alpha1.ReasonClusterPolicyNotReady, "active ClusterModelTrustPolicy is not ready")
	}
	status.PolicyName = pol.Name
	status.PolicyDigest = pol.PolicyDigest

	// Stage 1: Validate OCI URI and AllowedRegistries
	ref, err := registry.ParseDigestPinnedURI(artifact.Spec.Artifact.URI)
	if err != nil {
		return failStatus(status, nowMeta, v1alpha1.ConditionArtifactResolved, v1alpha1.ReasonInvalidArtifactURI, err.Error())
	}
	if !registry.IsRegistryAllowed(ref, pol.AllowedRegistries) {
		return failStatus(
			status,
			nowMeta,
			v1alpha1.ConditionArtifactResolved,
			v1alpha1.ReasonUntrustedRegistry,
			fmt.Sprintf("registry %s/%s is not in policy allowedRegistries %v", ref.Registry, ref.Repository, pol.AllowedRegistries),
		)
	}
	expectedBundleID := strings.TrimSpace(artifact.Spec.Identity.BundleID)
	if err := verify.ValidateDigest(expectedBundleID); err != nil {
		return failStatus(status, nowMeta, v1alpha1.ConditionBundleIdentityVerified, v1alpha1.ReasonBundleIdentityMismatch, fmt.Sprintf("invalid spec.identity.bundleID %q: %v", expectedBundleID, err))
	}

	// Stage 2: Fetch OCI Subject Manifest & Extract Layer Descriptors (Zero Weight Blob Downloads)
	subjManifest, _, err := e.RegistryClient.FetchManifest(ctx, ref)
	if err != nil {
		return failStatus(status, nowMeta, v1alpha1.ConditionArtifactResolved, v1alpha1.ReasonInvalidArtifactURI, fmt.Sprintf("fetch OCI model manifest: %v", err))
	}
	status.ResolvedManifestDigest = ref.Digest

	if subjManifest.ArtifactType != "" && !registry.MatchesArtifactType(subjManifest.ArtifactType, registry.ArtifactTypeModelBundle) {
		return failStatus(
			status,
			nowMeta,
			v1alpha1.ConditionArtifactResolved,
			v1alpha1.ReasonInvalidBundleManifest,
			fmt.Sprintf("unexpected OCI manifest artifactType %q (expected %q)", subjManifest.ArtifactType, registry.ArtifactTypeModelBundle),
		)
	}
	if subjManifest.Config.MediaType != registry.MediaTypeBundleManifestConfig {
		return failStatus(
			status,
			nowMeta,
			v1alpha1.ConditionArtifactResolved,
			v1alpha1.ReasonInvalidBundleManifest,
			fmt.Sprintf("unexpected OCI config mediaType %q (expected %q)", subjManifest.Config.MediaType, registry.MediaTypeBundleManifestConfig),
		)
	}

	observedFiles := make([]verify.FileEntry, 0, len(subjManifest.Layers))
	for i, l := range subjManifest.Layers {
		relPath := strings.TrimSpace(l.Annotations[registry.AnnotationBundlePath])
		if relPath == "" {
			return failStatus(
				status,
				nowMeta,
				v1alpha1.ConditionBundleIdentityVerified,
				v1alpha1.ReasonInvalidBundleManifest,
				fmt.Sprintf("OCI layer %d (%s) is missing required annotation %q", i, l.Digest, registry.AnnotationBundlePath),
			)
		}
		role := strings.TrimSpace(l.Annotations[registry.AnnotationBundleRole])
		observedFiles = append(observedFiles, verify.FileEntry{
			Path:   relPath,
			Role:   role,
			SHA256: l.Digest,
			Size:   l.Size,
		})
	}
	setCondition(&status, nowMeta, v1alpha1.ConditionArtifactResolved, metav1.ConditionTrue, v1alpha1.ReasonVerified, fmt.Sprintf("resolved OCI manifest %s with %d layer descriptors", ref.Digest, len(observedFiles)))

	// Stage 3: Fetch bundle-manifest.json config blob & verify bundle-identity/v1 + JCS manifest_digest
	rawBundleManifest, err := e.RegistryClient.FetchMetadataBlob(ctx, ref, subjManifest.Config)
	if err != nil {
		return failStatus(status, nowMeta, v1alpha1.ConditionBundleIdentityVerified, v1alpha1.ReasonInvalidBundleManifest, fmt.Sprintf("fetch bundle-manifest.json config blob: %v", err))
	}

	bm, err := verify.VerifyBundleManifestBytes(rawBundleManifest, observedFiles, verify.ManifestVerifyOptions{
		AllowUnknownFiles:    pol.Requirements.AllowUnknownBundleFiles,
		ExpectedBaseBundleID: opts.ExpectedBaseBundleID,
	})
	if err != nil {
		reason := v1alpha1.ReasonInvalidBundleManifest
		if opts.ExpectedBaseBundleID != "" && strings.Contains(err.Error(), "LoRA adapter") {
			reason = v1alpha1.ReasonLoRABaseModelUnverified
		}
		return failStatus(status, nowMeta, v1alpha1.ConditionBundleIdentityVerified, reason, err.Error())
	}
	if bm.BundleID != expectedBundleID {
		return failStatus(
			status,
			nowMeta,
			v1alpha1.ConditionBundleIdentityVerified,
			v1alpha1.ReasonBundleIdentityMismatch,
			fmt.Sprintf("recomputed bundle_id %s does not match spec.identity.bundleID %s", bm.BundleID, expectedBundleID),
		)
	}
	if pol.Requirements.RequireVerifiedLoRABase && bm.HasLoRAAdapter {
		if bm.LoRAVerificationStatus != "VERIFIED" || bm.VerifiedBaseBundleID == "" {
			return failStatus(
				status,
				nowMeta,
				v1alpha1.ConditionBundleIdentityVerified,
				v1alpha1.ReasonLoRABaseModelUnverified,
				fmt.Sprintf("LoRA adapter bundle has unverified base model relationship (status=%s)", bm.LoRAVerificationStatus),
			)
		}
		if opts.ExpectedBaseBundleID == "" {
			return failStatus(
				status,
				nowMeta,
				v1alpha1.ConditionBundleIdentityVerified,
				v1alpha1.ReasonLoRABaseModelUnverified,
				"LoRA adapter ModelArtifact must specify spec.runtime.baseModelArtifact referencing a verified base ModelArtifact",
			)
		}
	}

	status.VerifiedBundleID = bm.BundleID
	status.BundleManifestDigest = bm.ManifestDigest
	status.LoRAVerificationStatus = bm.LoRAVerificationStatus
	status.VerifiedBaseBundleID = bm.VerifiedBaseBundleID
	setCondition(&status, nowMeta, v1alpha1.ConditionBundleIdentityVerified, metav1.ConditionTrue, v1alpha1.ReasonVerified, fmt.Sprintf("verified bundle_id %s and JCS manifest_digest %s", bm.BundleID, bm.ManifestDigest))

	// Stage 4: OpenSSF Model Signing (OMS v1.0) Referrer Verification
	var omsRes verify.OMSResult
	if pol.Requirements.RequireOMSSignature {
		omsRefDigest, err := e.RegistryClient.SelectReferrerDigest(ctx, ref, registry.ArtifactTypeOMSSignature, artifact.Spec.EvidenceRefs.OMSSignatureDigest)
		if err != nil {
			return failStatus(status, nowMeta, v1alpha1.ConditionOMSSignatureVerified, v1alpha1.ReasonOMSSignatureInvalid, err.Error())
		}
		omsManifest, err := e.RegistryClient.ResolveReferrerManifest(ctx, ref, omsRefDigest, registry.ArtifactTypeOMSSignature)
		if err != nil {
			return failStatus(status, nowMeta, v1alpha1.ConditionOMSSignatureVerified, v1alpha1.ReasonOMSSignatureInvalid, err.Error())
		}
		if len(omsManifest.Layers) != 1 {
			return failStatus(status, nowMeta, v1alpha1.ConditionOMSSignatureVerified, v1alpha1.ReasonOMSSignatureInvalid, fmt.Sprintf("OMS referrer %s must have exactly 1 layer, got %d", omsRefDigest, len(omsManifest.Layers)))
		}
		rawOMS, err := e.RegistryClient.FetchMetadataBlob(ctx, ref, omsManifest.Layers[0])
		if err != nil {
			return failStatus(status, nowMeta, v1alpha1.ConditionOMSSignatureVerified, v1alpha1.ReasonOMSSignatureInvalid, fmt.Sprintf("fetch OMS signature blob: %v", err))
		}
		omsRes, err = verify.VerifyOMSBytes(rawOMS, observedFiles, verify.OMSVerifyOptions{
			TrustedModelSigners: pol.ModelSigners,
			UntrustedCandidates: pol.AllDomainCandidates,
		})
		if err != nil || omsRes.Status != verify.StatusVerified {
			reason := v1alpha1.ReasonOMSSignatureInvalid
			if omsRes.Status == verify.StatusUntrusted || errors.Is(err, verify.ErrUntrustedSigner) {
				reason = v1alpha1.ReasonUntrustedModelSigner
			}
			msg := omsRes.Reason
			if msg == "" && err != nil {
				msg = err.Error()
			}
			return failStatus(status, nowMeta, v1alpha1.ConditionOMSSignatureVerified, reason, msg)
		}
		status.OMSSignatureDigest = omsRes.SignatureDigest
		status.OMSSignerFingerprint = omsRes.SignerFingerprint
		setCondition(&status, nowMeta, v1alpha1.ConditionOMSSignatureVerified, metav1.ConditionTrue, v1alpha1.ReasonVerified, omsRes.Reason)
	}

	// Stage 5: CycloneDX 1.7 AI/ML-BOM Referrer Verification
	var bomRes verify.BOMResult
	if pol.Requirements.RequireCycloneDXBOM {
		bomRefDigest, err := e.RegistryClient.SelectReferrerDigest(ctx, ref, registry.ArtifactTypeCycloneDXBOM, artifact.Spec.EvidenceRefs.BOMArtifactDigest)
		if err != nil {
			return failStatus(status, nowMeta, v1alpha1.ConditionBOMVerified, v1alpha1.ReasonBOMVerificationFailed, err.Error())
		}
		bomManifest, err := e.RegistryClient.ResolveReferrerManifest(ctx, ref, bomRefDigest, registry.ArtifactTypeCycloneDXBOM)
		if err != nil {
			return failStatus(status, nowMeta, v1alpha1.ConditionBOMVerified, v1alpha1.ReasonBOMVerificationFailed, err.Error())
		}
		if len(bomManifest.Layers) != 1 {
			return failStatus(status, nowMeta, v1alpha1.ConditionBOMVerified, v1alpha1.ReasonBOMVerificationFailed, fmt.Sprintf("CycloneDX BOM referrer %s must have exactly 1 layer, got %d", bomRefDigest, len(bomManifest.Layers)))
		}
		rawBOM, err := e.RegistryClient.FetchMetadataBlob(ctx, ref, bomManifest.Layers[0])
		if err != nil {
			return failStatus(status, nowMeta, v1alpha1.ConditionBOMVerified, v1alpha1.ReasonBOMVerificationFailed, fmt.Sprintf("fetch CycloneDX BOM blob: %v", err))
		}
		bomRes, err = verify.VerifyBOMBytes(rawBOM, &bm, observedFiles)
		if err != nil || bomRes.Status != verify.StatusVerified {
			msg := bomRes.Reason
			if msg == "" && err != nil {
				msg = err.Error()
			}
			return failStatus(status, nowMeta, v1alpha1.ConditionBOMVerified, v1alpha1.ReasonBOMVerificationFailed, msg)
		}
		status.BOMCanonicalDigest = bomRes.CanonicalHash
		setCondition(&status, nowMeta, v1alpha1.ConditionBOMVerified, metav1.ConditionTrue, v1alpha1.ReasonVerified, bomRes.Reason)
	}

	// Stage 6: Dual-Layer LLM Release Gate Evaluation Attestation + Report Verification
	var evalRes verify.EvaluationSummary
	if pol.Requirements.RequireEvaluationGatePass {
		evalRefDigest, err := e.RegistryClient.SelectReferrerDigest(ctx, ref, registry.ArtifactTypeEvaluationAttestation, artifact.Spec.EvidenceRefs.EvaluationAttestationDigest)
		if err != nil {
			return failStatus(status, nowMeta, v1alpha1.ConditionEvaluationVerified, v1alpha1.ReasonEvaluationGateFailed, err.Error())
		}
		evalManifest, err := e.RegistryClient.ResolveReferrerManifest(ctx, ref, evalRefDigest, registry.ArtifactTypeEvaluationAttestation)
		if err != nil {
			return failStatus(status, nowMeta, v1alpha1.ConditionEvaluationVerified, v1alpha1.ReasonEvaluationGateFailed, err.Error())
		}
		if len(evalManifest.Layers) < 2 {
			return failStatus(
				status,
				nowMeta,
				v1alpha1.ConditionEvaluationVerified,
				v1alpha1.ReasonEvaluationGateFailed,
				fmt.Sprintf("evaluation referrer %s must contain both evaluation.dsse.json and report.json layers (got %d layers)", evalRefDigest, len(evalManifest.Layers)),
			)
		}
		var attDesc, repDesc *registry.Descriptor
		for i := range evalManifest.Layers {
			l := &evalManifest.Layers[i]
			switch l.Annotations[registry.AnnotationEvidenceRole] {
			case "evaluation-attestation":
				attDesc = l
			case "evaluation-report":
				repDesc = l
			default:
				if l.MediaType == registry.MediaTypeDSSELayer && attDesc == nil {
					attDesc = l
				} else if l.MediaType == registry.MediaTypeEvaluationReportLayer && repDesc == nil {
					repDesc = l
				}
			}
		}
		if attDesc == nil || repDesc == nil {
			return failStatus(status, nowMeta, v1alpha1.ConditionEvaluationVerified, v1alpha1.ReasonEvaluationGateFailed, "evaluation referrer is missing evaluation-attestation or evaluation-report layer")
		}
		rawEvalAtt, err := e.RegistryClient.FetchMetadataBlob(ctx, ref, *attDesc)
		if err != nil {
			return failStatus(status, nowMeta, v1alpha1.ConditionEvaluationVerified, v1alpha1.ReasonEvaluationGateFailed, fmt.Sprintf("fetch evaluation attestation blob: %v", err))
		}
		rawReport, err := e.RegistryClient.FetchMetadataBlob(ctx, ref, *repDesc)
		if err != nil {
			return failStatus(status, nowMeta, v1alpha1.ConditionEvaluationVerified, v1alpha1.ReasonEvaluationGateFailed, fmt.Sprintf("fetch evaluation report blob: %v", err))
		}

		evalRes, err = verify.VerifyEvaluationBytes(rawReport, rawEvalAtt, verify.EvalVerifyOptions{
			TrustedEvaluators:      pol.Evaluators,
			UntrustedCandidates:    pol.AllDomainCandidates,
			ExpectedBundleID:       bm.BundleID,
			ExpectedSuiteDigest:    pol.Requirements.ExpectedSuiteDigest,
			ExpectedDatasetDigest:  pol.Requirements.ExpectedDatasetDigest,
			MaxAge:                 pol.MaxEvidenceAge,
			RequireArtifactBinding: true,
			RequireTrustedEvidence: pol.Requirements.RequireNativeEvaluatorBinding,
			AllowPostHocBinding:    !pol.Requirements.RejectPostHocBinding,
			Now:                    now,
		})
		if err != nil || evalRes.Status != verify.StatusVerified || !evalRes.EligibleForRelease {
			reason := v1alpha1.ReasonEvaluationGateFailed
			if evalRes.Status == verify.StatusUntrusted || errors.Is(err, verify.ErrUntrustedSigner) {
				reason = v1alpha1.ReasonUntrustedEvaluator
			} else if evalRes.BindingMode == "POST_HOC_OPERATOR" || evalRes.AttestationKind == "OPERATOR_BINDING_ENDORSEMENT" || strings.Contains(evalRes.Reason, "post-hoc") {
				reason = v1alpha1.ReasonPostHocEvaluationRejected
			}
			msg := evalRes.Reason
			if msg == "" && err != nil {
				msg = err.Error()
			}
			return failStatus(status, nowMeta, v1alpha1.ConditionEvaluationVerified, reason, msg)
		}
		status.EvaluationReportDigest = evalRes.ReportDigest
		status.EvaluationAttestationDigest = evalRes.AttestationDigest
		status.EvaluationGateStatus = evalRes.EffectiveGateStatus
		status.EvaluationBindingMode = evalRes.BindingMode
		status.EvaluationSignerFingerprint = evalRes.SignerFingerprint
		setCondition(&status, nowMeta, v1alpha1.ConditionEvaluationVerified, metav1.ConditionTrue, v1alpha1.ReasonVerified, evalRes.Reason)
	}

	// Stage 7: Release Qualification Attestation & 5-Point Cross-Evidence Binding Verification
	var relRes verify.VerifiedQualification
	if pol.Requirements.RequireReleaseQualification {
		relRefDigest, err := e.RegistryClient.SelectReferrerDigest(ctx, ref, registry.ArtifactTypeReleaseQualification, artifact.Spec.EvidenceRefs.ReleaseQualificationDigest)
		if err != nil {
			return failStatus(status, nowMeta, v1alpha1.ConditionReleaseQualificationPassed, v1alpha1.ReasonReleaseQualificationFailed, err.Error())
		}
		relManifest, err := e.RegistryClient.ResolveReferrerManifest(ctx, ref, relRefDigest, registry.ArtifactTypeReleaseQualification)
		if err != nil {
			return failStatus(status, nowMeta, v1alpha1.ConditionReleaseQualificationPassed, v1alpha1.ReasonReleaseQualificationFailed, err.Error())
		}
		if len(relManifest.Layers) != 1 {
			return failStatus(status, nowMeta, v1alpha1.ConditionReleaseQualificationPassed, v1alpha1.ReasonReleaseQualificationFailed, fmt.Sprintf("release qualification referrer %s must have 1 layer, got %d", relRefDigest, len(relManifest.Layers)))
		}
		rawRelAtt, err := e.RegistryClient.FetchMetadataBlob(ctx, ref, relManifest.Layers[0])
		if err != nil {
			return failStatus(status, nowMeta, v1alpha1.ConditionReleaseQualificationPassed, v1alpha1.ReasonReleaseQualificationFailed, fmt.Sprintf("fetch release qualification blob: %v", err))
		}

		relRes, err = verify.VerifyReleaseQualificationBytes(rawRelAtt, verify.ReleaseVerifyOptions{
			TrustedAuthorities:            pol.ReleaseAuthorities,
			UntrustedCandidates:           pol.AllDomainCandidates,
			ExpectedBundleID:              bm.BundleID,
			ExpectedManifestDigest:        bm.ManifestDigest,
			ExpectedBOMDigest:             bomRes.CanonicalHash,
			ExpectedOMSSignatureDigest:    omsRes.SignatureDigest,
			ExpectedEvalReportDigest:      evalRes.ReportDigest,
			ExpectedEvalAttestationDigest: evalRes.AttestationDigest,
			RequireTrustedEvaluator:       pol.Requirements.RequireNativeEvaluatorBinding,
			RejectPostHocBinding:          pol.Requirements.RejectPostHocBinding,
			MaxAge:                        pol.MaxEvidenceAge,
			Now:                           now,
		})
		if err != nil || relRes.Status != verify.StatusVerified {
			reason := v1alpha1.ReasonReleaseQualificationFailed
			if relRes.Status == verify.StatusUntrusted || errors.Is(err, verify.ErrUntrustedSigner) {
				reason = v1alpha1.ReasonUntrustedReleaseAuthority
			} else if strings.Contains(relRes.Reason, "does not match") || (err != nil && strings.Contains(err.Error(), "does not match")) {
				reason = v1alpha1.ReasonCrossEvidenceMismatch
			}
			msg := relRes.Reason
			if msg == "" && err != nil {
				msg = err.Error()
			}
			return failStatus(status, nowMeta, v1alpha1.ConditionReleaseQualificationPassed, reason, msg)
		}

		// Verify optional RuntimeImage & ServingConfigDigest bindings between Spec and Attestation
		specImg := strings.TrimSpace(artifact.Spec.Runtime.RuntimeImage)
		attImg := strings.TrimSpace(relRes.RuntimeImage)
		if pol.Requirements.RequireImmutableRuntimeImage {
			effectiveImg := attImg
			if effectiveImg == "" {
				effectiveImg = specImg
			}
			if effectiveImg == "" {
				return failStatus(status, nowMeta, v1alpha1.ConditionReleaseQualificationPassed, v1alpha1.ReasonMutableRuntimeImage, "policy requires immutable runtimeImage in release qualification or ModelArtifact.spec.runtime")
			}
			if _, imgErr := registry.ParseDigestPinnedURI(effectiveImg); imgErr != nil {
				return failStatus(status, nowMeta, v1alpha1.ConditionReleaseQualificationPassed, v1alpha1.ReasonMutableRuntimeImage, fmt.Sprintf("runtimeImage %q is not pinned by immutable sha256 digest: %v", effectiveImg, imgErr))
			}
			if specImg != "" && attImg != "" && specImg != attImg {
				return failStatus(status, nowMeta, v1alpha1.ConditionReleaseQualificationPassed, v1alpha1.ReasonRuntimeImageMismatch, fmt.Sprintf("spec.runtime.runtimeImage %q does not match attested runtime_image %q", specImg, attImg))
			}
		}

		specCfg := strings.TrimSpace(artifact.Spec.Runtime.ServingConfigDigest)
		attCfg := strings.TrimSpace(relRes.ServingConfigDigest)
		if pol.Requirements.RequireServingConfigBinding {
			effectiveCfg := attCfg
			if effectiveCfg == "" {
				effectiveCfg = specCfg
			}
			if err := verify.ValidateDigest(effectiveCfg); err != nil {
				return failStatus(status, nowMeta, v1alpha1.ConditionReleaseQualificationPassed, v1alpha1.ReasonServingConfigDigestMismatch, "policy requires valid servingConfigDigest in release qualification or ModelArtifact.spec.runtime")
			}
			if specCfg != "" && attCfg != "" && specCfg != attCfg {
				return failStatus(status, nowMeta, v1alpha1.ConditionReleaseQualificationPassed, v1alpha1.ReasonServingConfigDigestMismatch, fmt.Sprintf("spec.runtime.servingConfigDigest %q does not match attested serving_config_digest %q", specCfg, attCfg))
			}
		}

		status.ReleaseQualificationDigest = relRes.AttestationDigest
		status.ReleaseAuthorityFingerprint = relRes.SignerFingerprint
		if attImg != "" {
			status.RuntimeImage = attImg
		} else {
			status.RuntimeImage = specImg
		}
		if attCfg != "" {
			status.ServingConfigDigest = attCfg
		} else {
			status.ServingConfigDigest = specCfg
		}
		setCondition(&status, nowMeta, v1alpha1.ConditionReleaseQualificationPassed, metav1.ConditionTrue, v1alpha1.ReasonVerified, relRes.Reason)
	}

	validUntil := metav1.NewTime(now.Add(pol.VerificationTTL))
	status.Verified = true
	status.LastVerifiedAt = &nowMeta
	status.ValidUntil = &validUntil
	status.FailureReason = ""
	status.FailureMessage = ""
	setCondition(&status, nowMeta, v1alpha1.ConditionReady, metav1.ConditionTrue, v1alpha1.ReasonVerified, "all model supply-chain provenance, evaluation, and release qualification checks passed")
	return status
}

func failStatus(status v1alpha1.ModelArtifactStatus, now metav1.Time, failedCond string, reason string, msg string) v1alpha1.ModelArtifactStatus {
	status.Verified = false
	status.FailureReason = reason
	status.FailureMessage = msg
	if failedCond != v1alpha1.ConditionReady {
		setCondition(&status, now, failedCond, metav1.ConditionFalse, reason, msg)
	}
	setCondition(&status, now, v1alpha1.ConditionReady, metav1.ConditionFalse, reason, msg)
	return status
}

func setCondition(status *v1alpha1.ModelArtifactStatus, now metav1.Time, condType string, condStatus metav1.ConditionStatus, reason, message string) {
	newCond := metav1.Condition{
		Type:               condType,
		Status:             condStatus,
		ObservedGeneration: status.ObservedGeneration,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}
	for i := range status.Conditions {
		if status.Conditions[i].Type == condType {
			if status.Conditions[i].Status != condStatus {
				status.Conditions[i].LastTransitionTime = now
			}
			status.Conditions[i].Status = condStatus
			status.Conditions[i].ObservedGeneration = status.ObservedGeneration
			status.Conditions[i].Reason = reason
			status.Conditions[i].Message = message
			return
		}
	}
	status.Conditions = append(status.Conditions, newCond)
}
