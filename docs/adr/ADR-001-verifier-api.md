# ADR-001: Public, Descriptor-Based Verification API

- **Status:** Accepted
- **Date:** 2026-11-04
- **Owners:** AI platform / provenance maintainers
- **Related project:** `ai-model-provenance-gate` (verified at HEAD `c9fa3b809ba6e1cd6ac90c899d27b7c3dc6a22da`)

## Context

`gke-model-admission-controller` must verify model bundle identity and signed evidence from OCI registry metadata without downloading multi-gigabyte `.safetensors` model weights to controller pod storage.

Inspection of `ai-model-provenance-gate` (`c9fa3b809ba6e1cd6ac90c899d27b7c3dc6a22da`) confirmed:
- Core primitives (`internal/identity.Compute`, `internal/manifest.Finalize`, `internal/bom.Validate`, `internal/verification.VerifyInTotoDSSE`) already operate on in-memory structures or `[]byte`.
- However, higher-level verification entry points (`internal/oms.VerifyBundle`, `internal/attestation.VerifyQualificationFile`, `internal/evaluation/llmreleasegate.Verify`, `internal/deployment.Verify`) currently accept filesystem paths and invoke `os.ReadFile` or walk local directories (`internal/inventory.Build`).
- Furthermore, all packages reside under `internal/`, which Go module visibility rules prevent external repositories from importing.

Invoking the `ai-model-provenance` CLI as a subprocess inside the Kubernetes operator is rejected: it introduces process execution overhead, temporary-file materialization, exit-code parsing, and security surface area on every reconciliation.

## Decision

Expose a small, stable, disk-I/O-free and network-I/O-free Go package at `github.com/devenes/ai-model-provenance-gate/pkg/verify`.

The package accepts:
- OCI descriptor-derived file inventories (`[]FileEntry`).
- In-memory manifest, BOM, evaluation report, and DSSE/Sigstore attestation bytes (`[]byte`).
- Explicit, domain-separated trusted signer sets (`[]TrustedSignerRule`) with raw PEM public keys (`[]byte`).
- Parsed verification options.

### Frozen API Surface

```go
package verify

import "time"

// Status represents the deterministic outcome of a cryptographic or policy verification step.
type Status string

const (
	StatusVerified  Status = "VERIFIED"
	StatusInvalid   Status = "INVALID"
	StatusUntrusted Status = "UNTRUSTED"
	StatusMissing   Status = "MISSING"
	StatusBlocked   Status = "BLOCKED"
	StatusNotRun    Status = "NOT_RUN"
)

// FileEntry represents a single model bundle file derived from an OCI layer descriptor.
type FileEntry struct {
	Path   string `json:"path"`
	Role   string `json:"role,omitempty"` // Optional on input; deterministically classified via ClassifyFile(Path)
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// TrustedSignerRule defines an authorized public key within a specific trust domain.
type TrustedSignerRule struct {
	ID                       string `json:"id"`
	Name                     string `json:"name,omitempty"`
	Role                     string `json:"role,omitempty"`
	AllowOperatorEndorsement bool   `json:"allow_operator_endorsement,omitempty"`
	KeyID                    string `json:"key_id,omitempty"`
	KeyFingerprint           string `json:"key_fingerprint,omitempty"` // Optional "sha256:<64-hex>" constraint
	PublicKeyPEM             []byte `json:"-"`
}

// KeyInfo holds the parsed PKIX metadata and deterministic SHA-256 fingerprint of a public key.
type KeyInfo struct {
	KeyID       string `json:"key_id"`
	Name        string `json:"name,omitempty"`
	Fingerprint string `json:"fingerprint"` // "sha256:<64-hex>" over PKIX DER bytes
	Algorithm   string `json:"algorithm"`
}

// IdentityResult contains the computed bundle-identity/v1 digest and normalized file entries.
type IdentityResult struct {
	SchemaVersion string      `json:"schema_version"`
	BundleID      string      `json:"bundle_id"`
	FileCount     int         `json:"file_count"`
	TotalBytes    int64       `json:"total_bytes"`
	Files         []FileEntry `json:"files"`
	CanonicalJSON []byte      `json:"-"`
}

// ManifestVerifyOptions configures bundle-manifest.json validation.
type ManifestVerifyOptions struct {
	AllowUnknownFiles    bool
	ExpectedBaseBundleID string
}

// BundleManifest holds the verified bundle-identity/v1 manifest metadata needed for downstream checks.
type BundleManifest struct {
	SchemaVersion           string      `json:"schema_version"`
	BundleID                string      `json:"bundle_id"`
	ManifestDigest          string      `json:"manifest_digest"` // RFC 8785 JCS SHA-256 with manifest_digest=""
	ConfigBlobDigest        string      `json:"config_blob_digest"` // Raw SHA-256 of the manifest JSON bytes
	FileCount               int         `json:"file_count"`
	TotalBytes              int64       `json:"total_bytes"`
	PrimaryFormat           string      `json:"primary_format"`
	HasLoRAAdapter          bool        `json:"has_lora_adapter"`
	LoRAVerificationStatus  string      `json:"lora_verification_status"`
	VerifiedBaseBundleID    string      `json:"verified_base_bundle_id,omitempty"`
	DeclaredBaseModel       string      `json:"declared_base_model,omitempty"`
	ContainsSymlinks        bool        `json:"contains_symlinks"`
	ContainsUnsafePickle    bool        `json:"contains_unsafe_pickle"`
	ContainsRemoteCodeHooks bool        `json:"contains_remote_code_hooks"`
	UnknownRegularFiles     []string    `json:"unknown_regular_files,omitempty"`
	Files                   []FileEntry `json:"files"`
	RawJSON                 []byte      `json:"-"`
}

// OMSVerifyOptions configures in-memory OpenSSF Model Signing (OMS v1.0) verification.
type OMSVerifyOptions struct {
	TrustedModelSigners []TrustedSignerRule
	UntrustedCandidates []TrustedSignerRule // Populated from other trust domains to detect cross-domain key misuse
	IgnoreUnsignedFiles bool
}

// OMSResult captures the outcome of OMS v1.0 verification against OCI descriptors.
type OMSResult struct {
	Status                Status   `json:"status"`
	SignatureDigest       string   `json:"signature_digest"` // Raw byte SHA-256 of the OMS envelope
	PredicateType         string   `json:"predicate_type,omitempty"`
	RootDigest            string   `json:"root_digest,omitempty"` // OMS v1.0 §6.5.1 root digest ("sha256:<hex>")
	IsSigstoreBundle      bool     `json:"is_sigstore_bundle"`
	SignerID              string   `json:"signer_id,omitempty"`
	SignerKeyID           string   `json:"signer_key_id,omitempty"`
	SignerFingerprint     string   `json:"signer_fingerprint,omitempty"`
	Algorithm             string   `json:"algorithm,omitempty"`
	VerifiedFileCount     int      `json:"verified_file_count"`
	DigestsMatchInventory bool     `json:"digests_match_inventory"`
	MismatchedFiles       []string `json:"mismatched_files,omitempty"`
	MissingSignedFiles    []string `json:"missing_signed_files,omitempty"`
	UnsignedBundleFiles   []string `json:"unsigned_bundle_files,omitempty"`
	Reason                string   `json:"reason"`
}

// BOMResult captures the outcome of CycloneDX 1.6/1.7 AI/ML-BOM verification.
type BOMResult struct {
	Status        Status `json:"status"`
	CanonicalHash string `json:"canonical_hash"` // RFC 8785 JCS SHA-256 matching ReleaseQualificationPredicate.BOM.Digest
	RawBlobDigest string `json:"raw_blob_digest"`
	SpecVersion   string `json:"spec_version"`
	SerialNumber  string `json:"serial_number"`
	BundleID      string `json:"bundle_id"`
	Reason        string `json:"reason"`
}

// EvalVerifyOptions configures LLM Release Gate report + evaluation-evidence/v1 attestation verification.
type EvalVerifyOptions struct {
	TrustedEvaluators      []TrustedSignerRule
	UntrustedCandidates    []TrustedSignerRule
	ExpectedBundleID       string
	ExpectedSuiteDigest    string
	ExpectedDatasetDigest  string
	MaxAge                 time.Duration
	RequireArtifactBinding bool
	RequireTrustedEvidence bool
	AllowPostHocBinding    bool
	Now                    time.Time
}

// EvaluationSummary holds the 7 orthogonal evaluation verification guarantees.
type EvaluationSummary struct {
	Status                       Status `json:"status"`
	ReportDigest                 string `json:"report_digest"`      // CRLF->LF normalized SHA-256 of report.json
	AttestationDigest            string `json:"attestation_digest"` // Raw byte SHA-256 of evaluation.dsse.json
	AttestationKind              string `json:"attestation_kind,omitempty"`
	SchemaVersion                string `json:"schema_version"`
	RunID                        string `json:"run_id"`
	Timestamp                    string `json:"timestamp"`
	SuiteDigest                  string `json:"suite_digest"`
	DatasetDigest                string `json:"dataset_digest"`
	ReportedGateStatus           string `json:"reported_gate_status"`
	EffectiveGateStatus          string `json:"effective_gate_status"`
	EvidenceQuality              string `json:"evidence_quality"`
	BindingStatus                string `json:"binding_status"`
	BindingMode                  string `json:"binding_mode"`
	TrustStatus                  string `json:"trust_status"`
	DeclaredBundleID             string `json:"declared_bundle_id,omitempty"`
	AttestedBundleID             string `json:"attested_bundle_id,omitempty"`
	ExpectedBundleID             string `json:"expected_bundle_id"`
	ReportIntegrityVerified      bool   `json:"report_integrity_verified"`
	ReportAuthenticityVerified   bool   `json:"report_authenticity_verified"`
	ProducerIdentityVerified     bool   `json:"producer_identity_verified"`
	ArtifactBindingVerified      bool   `json:"artifact_binding_verified"`
	ExecutionEvidenceVerified    bool   `json:"execution_evidence_verified"`
	SuiteDatasetIdentityVerified bool   `json:"suite_dataset_identity_verified"`
	FreshnessVerified            bool   `json:"freshness_verified"`
	EligibleForRelease           bool   `json:"eligible_for_release"`
	SignerID                     string `json:"signer_id,omitempty"`
	SignerFingerprint            string `json:"signer_fingerprint,omitempty"`
	Reason                       string `json:"reason"`
}

// ReleaseVerifyOptions configures release-qualification/v1 verification and cross-evidence binding checks.
type ReleaseVerifyOptions struct {
	TrustedAuthorities            []TrustedSignerRule
	UntrustedCandidates           []TrustedSignerRule
	ExpectedBundleID              string
	ExpectedManifestDigest        string // JCS manifest_digest from BundleManifest
	ExpectedBOMDigest             string // JCS canonical_hash from BOMResult
	ExpectedOMSSignatureDigest    string // Raw signature_digest from OMSResult
	ExpectedEvalReportDigest      string // Normalized report_digest from EvaluationSummary
	ExpectedEvalAttestationDigest string // Raw attestation_digest from EvaluationSummary
	RequireTrustedEvaluator       bool
	RejectPostHocBinding          bool
	MaxAge                        time.Duration
	Now                           time.Time
}

// VerifiedQualification holds the verified release-qualification/v1 result and extracted bindings.
type VerifiedQualification struct {
	Status                  Status `json:"status"`
	AttestationDigest       string `json:"attestation_digest"` // Raw byte SHA-256 of release-qualification DSSE
	Timestamp               string `json:"timestamp"`
	SignerID                string `json:"signer_id,omitempty"`
	SignerKeyID             string `json:"signer_key_id,omitempty"`
	SignerFingerprint       string `json:"signer_fingerprint,omitempty"`
	SubjectBundleID         string `json:"subject_bundle_id"`
	ManifestDigest          string `json:"manifest_digest"`
	BOMDigest               string `json:"bom_digest"`
	OMSSignatureDigest      string `json:"oms_signature_digest"`
	OMSSignerFingerprint    string `json:"oms_signer_fingerprint,omitempty"`
	EvalReportDigest        string `json:"eval_report_digest,omitempty"`
	EvalAttestationDigest   string `json:"eval_attestation_digest,omitempty"`
	EvalSuiteDigest         string `json:"eval_suite_digest,omitempty"`
	EvalDatasetDigest       string `json:"eval_dataset_digest,omitempty"`
	EvalSignerFingerprint   string `json:"eval_signer_fingerprint,omitempty"`
	LoRAStatus              string `json:"lora_status"`
	VerifiedBaseBundleID    string `json:"verified_base_bundle_id,omitempty"`
	RuntimeImage            string `json:"runtime_image,omitempty"`
	ServingConfigDigest     string `json:"serving_config_digest,omitempty"`
	QualificationPolicyHash string `json:"qualification_policy_hash"`
	GateStatus              string `json:"gate_status"`
	Reason                  string `json:"reason"`
}

func ParsePublicKeyPEM(rawPEM []byte, keyID string, name string) (KeyInfo, error)
func ClassifyFileRole(relPath string) (string, error)
func ComputeBundleIdentityFromDescriptors(files []FileEntry) (IdentityResult, error)
func VerifyBundleManifestBytes(manifestJSON []byte, observedFiles []FileEntry, opts ManifestVerifyOptions) (BundleManifest, error)
func VerifyOMSBytes(rawEnvelope []byte, observedFiles []FileEntry, opts OMSVerifyOptions) (OMSResult, error)
func VerifyBOMBytes(rawBOM []byte, expectedManifest *BundleManifest, observedFiles []FileEntry) (BOMResult, error)
func VerifyEvaluationBytes(rawReport []byte, rawAttestation []byte, opts EvalVerifyOptions) (EvaluationSummary, error)
func VerifyReleaseQualificationBytes(rawAttestation []byte, opts ReleaseVerifyOptions) (VerifiedQualification, error)
```

## API Behavior and Security Requirements

1. **Descriptor-only inventory:** `ComputeBundleIdentityFromDescriptors` normalizes paths via `internal/inventory.NormalizeAndValidateRelPath`, rejects duplicate/colliding NFC or case-folded paths, classifies roles via `internal/inventory.ClassifyFile`, and computes `bundle-identity/v1` via RFC 8785 JCS (`internal/identity.Compute`).
2. **No hidden I/O:** None of the exported functions in `pkg/verify` read files from disk, open network sockets, or spawn external processes.
3. **Exact digest semantics preserved:**
   - `BundleManifest.ManifestDigest` is computed via RFC 8785 JCS with `manifest_digest: ""` (`internal/manifest.Finalize`), while `BundleManifest.ConfigBlobDigest` records the raw byte SHA-256 of `bundle-manifest.json`.
   - `BOMResult.CanonicalHash` is computed via `digest.ComputeBytes(identity.CanonicalizeJSON(rawBOM))` to match `ReleaseQualificationPredicate.BOM.Digest` (`internal/bom/bom.go:477-485`).
   - `EvaluationSummary.ReportDigest` is computed via `digest.ComputeNormalizedText(rawReport)` (`internal/evaluation/llmreleasegate/adapter.go:373`).
   - `OMSResult.SignatureDigest`, `EvaluationSummary.AttestationDigest`, and `VerifiedQualification.AttestationDigest` are computed via `digest.ComputeBytes(rawEnvelope)`.
4. **Trust-domain isolation:** Each verification function accepts both its domain's `TrustedSigners` and `UntrustedCandidates` (populated from the other two trust domains) so cross-domain key misuse is deterministically reported as `StatusUntrusted` (`ErrUntrustedSigner`).
5. **Cross-evidence binding in `VerifyReleaseQualificationBytes`:** Beyond verifying the DSSE signature and in-toto subject, `VerifyReleaseQualificationBytes` checks that the predicate's recorded `bundle.manifest_digest`, `bom.digest`, `oms_verification.signature_digest`, `evaluation.report_digest`, and `evaluation.attestation_digest` match the exact digests of the sibling evidence artifacts verified in the same reconciliation pass.
6. **No `internal/` leakage:** All exported types reside in `pkg/verify`.

## Alternatives Considered

### A. Invoke `ai-model-provenance` CLI from the controller
**Rejected.** Subprocess execution and temporary files are unnecessary for in-process verification and complicate the reconciler.

### B. Copy cryptographic verification code into the controller
**Rejected.** This duplicates critical security logic and risks divergence between CLI and cluster enforcement.

### C. Expose all existing `internal` packages
**Rejected.** This creates a broad and unstable API surface and leaks implementation details.

### D. Public descriptor/byte-oriented facade (`pkg/verify`)
**Accepted.** Reuses established internals while providing the exact zero-disk contract needed by OCI verification.

## Consequences

**Positive**
- Zero model weight downloads required for descriptor-based verification.
- Existing CLI and GKE controller share one cryptographic implementation.
- Unit-testable verification core remains independent from Kubernetes.

**Trade-offs**
- Path-based internals are wrapped or refactored to delegate to byte/inventory primitives.
- Public types and error semantics require semantic versioning discipline.

## Acceptance Criteria

- Unit test proves `ComputeBundleIdentityFromDescriptors` produces the exact same `bundle_id` as `bundle.Inspect` on local golden fixtures.
- `VerifyBundleManifestBytes`, `VerifyOMSBytes`, `VerifyBOMBytes`, `VerifyEvaluationBytes`, and `VerifyReleaseQualificationBytes` pass on valid in-memory fixtures without disk reads.
- Cross-domain key reuse, tampered payloads, mismatched descriptors, post-hoc bindings, and transplanted evidence digests fail closed with distinct typed errors and `Status` codes.
