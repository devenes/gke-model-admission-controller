# ADR-003: Release Qualification, Evidence Binding, and Trust Domains

- **Status:** Accepted
- **Date:** 2026-11-04
- **Owners:** AI platform / provenance maintainers
- **Related repositories:** `llm-release-gate` (`e3e1dc62cbbaeb24738d45e5cd84d133493eff59`), `ai-model-provenance-gate` (`c9fa3b809ba6e1cd6ac90c899d27b7c3dc6a22da`)

## Context

The GKE admission decision must enforce a complete, cryptographically bound supply-chain trust chain for the exact AI model bundle being deployed.

Source inspection of `ai-model-provenance-gate` and `llm-release-gate` confirmed:
- Model signing (`OMS v1.0`), behavioral evaluation (`evaluation-evidence/v1`), and release qualification (`release-qualification/v1`) represent three distinct security roles.
- In `ai-model-provenance-gate/internal/cli/cli.go:815-879`, keys from one domain (`signing.trusted_signers`) are explicitly passed as `CandidateUntrustedKeys` to the other domains so that signing an evaluation or release attestation with a model-signing key is rejected as `UNTRUSTED_SIGNER`.
- Each digest field inside `ReleaseQualificationPredicate` (`internal/attestation/attestation.go:38-132`) uses a specific canonicalization or normalization algorithm that must be matched bit-for-bit during verification.

## Decision

### 1. End-to-End Evidence Sequence

```text
Model Producer
  ├── Inspects bundle -> bundle-manifest.json (bundle-identity/v1)
  ├── Generates CycloneDX 1.7 AI/ML-BOM -> bom.cdx.json
  └── Signs model inventory -> oms.sig.json (OMS v1.0 DSSE / Sigstore bundle)
          │
          ▼
Automated LLM Evaluator (llm-release-gate run)
  ├── Validates bundle-manifest.json and candidate.metadata.model_bundle_id
  ├── Executes evaluation suite & dataset -> report.json (llm-release-gate.report/v1)
  └── Signs evaluation-evidence/v1 at evaluation time -> evaluation.dsse.json
      (binding_mode: NATIVE_CI_EVALUATOR, attestation_kind: TRUSTED_EVALUATOR_ATTESTATION)
          │
          ▼
Release Qualification Gate (ai-model-provenance gate evaluate)
  ├── Verifies bundle-identity/v1, OMS v1.0, CycloneDX 1.7 BOM, and native evaluation DSSE
  ├── Binds runtime_image (@sha256:...) and serving_config_digest
  └── Signs release-qualification/v1 DSSE -> release-qualification.dsse.json
          │
          ▼
OCI Registry Packaging
  ├── Pushes OCI Model Manifest (config=bundle-manifest.json, layers=model files)
  └── Pushes 4 OCI Referrer Manifests bound to subject.digest == OCI Model Manifest digest
          │
          ▼
GKE ModelArtifact Controller & ValidatingAdmissionWebhook
  ├── Reconciler verifies OCI descriptors + 4 referrers + cross-evidence digest bindings
  └── Webhook enforces Verified=True, policyDigest, validUntil, runtimeImage, and default-deny
```

### 2. Three Explicit Trust Domains

`ClusterModelTrustPolicy` separates signers into three non-overlapping trust domains:

```yaml
spec:
  trust:
    modelSigners:
      - id: model-signer-prod
        keyID: model-signer-1
        keyFingerprint: sha256:...
        publicKeySecretRef:
          name: model-trust-keys
          key: model-signer.pub
    evaluators:
      - id: evaluator-prod
        role: trusted-evaluator
        keyID: eval-signer-1
        keyFingerprint: sha256:...
        publicKeySecretRef:
          name: model-trust-keys
          key: eval-signer.pub
    releaseAuthorities:
      - id: release-authority-prod
        keyID: release-signer-1
        keyFingerprint: sha256:...
        publicKeySecretRef:
          name: model-trust-keys
          key: release-signer.pub
```

#### Trust-Domain Enforcement Rules
1. **No implicit cross-domain trust:** `modelSigners` only verify OMS v1.0 signatures; `evaluators` (with `role: trusted-evaluator`) only verify `evaluation-evidence/v1`; `releaseAuthorities` only verify `release-qualification/v1`.
2. **Cross-domain misuse detection:** When verifying any domain, public keys from the other two domains (that are not explicitly authorized in the target domain) are passed as `UntrustedCandidates` so cross-domain key misuse is flagged as `UNTRUSTED_SIGNER`.
3. **Deterministic Policy Digest:** The effective `policyDigest` (`sha256:<64-hex>`) is computed over the RFC 8785 JCS canonical representation of `ClusterModelTrustPolicy.spec` **plus** the resolved PKIX SHA-256 fingerprints (`sha256:<64-hex>`) of every referenced public key. Rotating any Secret key immediately changes `policyDigest` and invalidates prior verifications until re-reconciled.

### 3. Frozen Predicate URIs and Schema Versions

Confirmed against `ai-model-provenance-gate/internal/version/version.go` and `internal/oms/oms.go`:

| Artifact | Identifier Field | Exact Value |
|---|---|---|
| In-toto Statement | `_type` | `https://in-toto.io/Statement/v1` |
| DSSE Payload Type | `payloadType` | `application/vnd.in-toto+json` |
| OMS v1.0 Signature | `predicateType` | `https://model_signing/signature/v1.0` |
| Evaluation Evidence | `predicateType` | `https://github.com/devenes/ai-model-provenance-gate/attestation/evaluation-evidence/v1` |
| Release Qualification | `predicateType` | `https://github.com/devenes/ai-model-provenance-gate/attestation/release-qualification/v1` |
| Release Predicate Schema | `predicate.schema_version` | `release-qualification/v1` |
| Evaluation Predicate Schema | `predicate.schema_version` | `evaluation-evidence/v1` |
| Evaluation Report Schema | `schemaVersion` | `llm-release-gate.report/v1` |
| Bundle Identity Schema | `schema_version` | `bundle-identity/v1` |
| CycloneDX BOM Schema | `$schema` / `specVersion` | `http://cyclonedx.org/schema/bom-1.7.schema.json` (`1.7`) |

### 4. Native Evaluator-Time Signing Contract (`llm-release-gate`)

`llm-release-gate run` supports native evaluator-time evidence binding and signing:

```bash
llm-release-gate run \
  --suite ./suite.yaml \
  --candidate ./candidate.yaml \
  --bundle-manifest ./bundle-manifest.json \
  --signing-key ./keys/evaluator.key \
  --signing-key-id eval-signer-1 \
  --attestation-output ./out/evaluation.dsse.json \
  --output ./out/report.json
```

#### Exact Execution Semantics
1. When `--bundle-manifest` is supplied, `llm-release-gate` parses and validates `bundle-manifest.json`:
   - Verifies `schema_version == "bundle-identity/v1"`.
   - Verifies `bundle_id` format (`sha256:<64-hex>`) and recomputes both the JCS `bundle_id` from `files[]` and the JCS `manifest_digest` (with `manifest_digest: ""`) to prove the manifest was not tampered with.
   - Ensures `candidate.metadata.model_bundle_id` is set to `bundle_id` (or fails with exit code `2` if `candidate.yaml` declares a conflicting `model_bundle_id`).
2. After writing `report.json` and `report.md`, if `--signing-key` is supplied:
   - Computes `report_digest = ComputeNormalizedSHA256(reportJSONBytes)` (CRLF -> LF normalized SHA-256).
   - Constructs an in-toto Statement v1 with `subject[0] = {name: "model-bundle:" + candidate.name, digest: {"sha256": trimPrefix(bundle_id)}}` and `predicateType = "https://github.com/devenes/ai-model-provenance-gate/attestation/evaluation-evidence/v1"`.
   - Populates `EvaluationEvidencePredicate` with `schema_version: "evaluation-evidence/v1"`, `attestation_kind: "TRUSTED_EVALUATOR_ATTESTATION"`, `binding_mode: "NATIVE_CI_EVALUATOR"`, `model_bundle_id`, `report_digest`, `run_id`, `evaluation_timestamp`, `tool_version`, `gate_status`, `evidence_quality`, `suite_digest`, `dataset_digest`, `evaluator_name`, `evaluator_version`, `execution_mode`, `candidate_name`, `candidate_digest`, and `prompt_config_digest`.
   - Signs the statement using DSSE v1.0 PAE (`application/vnd.in-toto+json`) and writes `evaluation.dsse.json`.

### 5. Source-Verified Digest Semantics & Cross-Binding Matrix

Every digest recorded inside `ReleaseQualificationPredicate` must equal the corresponding verified artifact digest:

| Predicate Field in `release-qualification/v1` | Source File & Line in `ai-model-provenance-gate` | Exact Digest Algorithm |
|---|---|---|
| `statement.subject[0].digest.sha256` | `internal/attestation/attestation.go:267` | 64-hex of `bundle_id` (`sha256(JCS(CanonicalDocument))`) |
| `predicate.bundle.bundle_id` | `internal/attestation/attestation.go:196` | `sha256:<64-hex>` of `bundle-identity/v1` |
| `predicate.bundle.manifest_digest` | `internal/manifest/manifest.go:74` | `sha256:<64-hex>` over RFC 8785 JCS of `BundleManifest` with `manifest_digest: ""` |
| `predicate.bom.digest` | `internal/bom/bom.go:485` | `sha256:<64-hex>` over **RFC 8785 JCS canonicalized** `bom.cdx.json` bytes (`identity.CanonicalizeJSON`) |
| `predicate.oms_verification.signature_digest` | `internal/oms/oms.go:404` | `sha256:<64-hex>` over **raw bytes** of `oms.sig.json` (`digest.ComputeBytes`) |
| `predicate.evaluation.report_digest` | `internal/evaluation/llmreleasegate/adapter.go:373` | `sha256:<64-hex>` over **CRLF->LF normalized** `report.json` bytes (`digest.ComputeNormalizedText`) |
| `predicate.evaluation.attestation_digest` | `internal/evaluation/llmreleasegate/adapter.go:535` | `sha256:<64-hex>` over **raw bytes** of `evaluation.dsse.json` (`digest.ComputeBytes`) |
| `predicate.runtime.serving_config_digest` | `internal/cli/cli.go:894` | `sha256:<64-hex>` over **CRLF->LF normalized** serving config bytes (`digest.ComputeNormalizedText`) |
| `predicate.policy.digest` | `internal/config/config.go:187` | `sha256:<64-hex>` over **CRLF->LF normalized** `policy.yaml` bytes |

If any fetched OCI referrer artifact produces a digest that differs from the corresponding field in `ReleaseQualificationPredicate`, `VerifyReleaseQualificationBytes` rejects the release qualification as `StatusInvalid` (`ErrInvalidReleaseAttestation: evidence cross-binding mismatch`).

## Acceptance Criteria

- Trusted model signer key cannot verify an evaluation or release qualification attestation (`StatusUntrusted`).
- `llm-release-gate run` with `--bundle-manifest` and `--signing-key` produces `NATIVE_CI_EVALUATOR` / `TRUSTED_EVALUATOR_ATTESTATION` evidence that passes strict policy.
- Post-hoc operator binding (`POST_HOC_OPERATOR`) and unsigned reports (`UNVERIFIED_LOCAL_JSON`) are rejected in strict mode.
- Replacing any OCI referrer (BOM, OMS, or Evaluation) with a valid referrer from another run or model fails the `ReleaseQualificationPredicate` cross-binding check.
