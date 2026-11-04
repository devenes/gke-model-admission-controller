# ADR-002: OCI Model Artifact Layout and Evidence Referrers

- **Status:** Accepted
- **Date:** 2026-11-04
- **Owners:** AI platform / provenance maintainers
- **Related standards:** OCI Image Specification v1.1, OCI Distribution Specification v1.1 (Referrers API), ORAS

## Context

The GKE controller must inspect the immutable model artifact and associated cryptographic evidence without downloading multi-gigabyte model weight blobs.

Code inspection of `ai-model-provenance-gate` (`c9fa3b809ba6e1cd6ac90c899d27b7c3dc6a22da`) identified four architectural constraints for OCI packaging:
1. **Self-hashing prevention:** `bundle-manifest.json` contains `bundle_id` and `manifest_digest`. Including `bundle-manifest.json` inside the model file layers would alter `bundle_id`, classify as `unknown_file` under `internal/inventory.ClassifyFile`, trigger `BUNDLE_UNKNOWN_FILES_FORBIDDEN`, and fail 1-to-1 OMS/BOM file count checks.
2. **Path preservation:** `internal/inventory.Build` records paths relative to the bundle root (e.g., `config.json`, `model.safetensors`). Prepending an OCI-only `model/` prefix would change the computed `bundle_id`.
3. **Three distinct digest domains:** OCI manifest digest, `bundle_id` (`bundle-identity/v1`), and OMS v1.0 root digest (`internal/oms.ComputeRootDigest`) are distinct SHA-256 values over different inputs.
4. **Evaluation report + attestation pairing:** `internal/evaluation/llmreleasegate.Verify` (`adapter.go:480-576`) requires both `report.json` (`llm-release-gate.report/v1`) and `evaluation.dsse.json` (`evaluation-evidence/v1`) to verify all 7 evaluation guarantees (`ReportIntegrityVerified`, `ExecutionEvidenceVerified`, `ExecutedCases > 0`).

## Decision

### 1. OCI Model Artifact Manifest Structure

Use an OCI Image Manifest v1.1 (`application/vnd.oci.image.manifest.v1+json`) with `artifactType: application/vnd.devenes.ai-model-bundle.v1`, where `config` stores `bundle-manifest.json` and `layers[]` contains only the raw model bundle files:

```json
{
  "schemaVersion": 2,
  "mediaType": "application/vnd.oci.image.manifest.v1+json",
  "artifactType": "application/vnd.devenes.ai-model-bundle.v1",
  "config": {
    "mediaType": "application/vnd.devenes.bundle-manifest.v1+json",
    "digest": "sha256:<raw-bundle-manifest-json-sha256>",
    "size": 1842,
    "annotations": {
      "org.opencontainers.image.title": "bundle-manifest.json",
      "dev.gke.trust.bundle-id": "sha256:<bundle-identity-v1>"
    }
  },
  "layers": [
    {
      "mediaType": "application/vnd.devenes.model-file.v1+octet-stream",
      "digest": "sha256:<raw-file-sha256>",
      "size": 612,
      "annotations": {
        "org.opencontainers.image.title": "config.json"
      }
    },
    {
      "mediaType": "application/vnd.devenes.model-file.v1+octet-stream",
      "digest": "sha256:<raw-file-sha256>",
      "size": 4096,
      "annotations": {
        "org.opencontainers.image.title": "model.safetensors"
      }
    }
  ]
}
```

### 2. Raw File Blob & Descriptor Rules

Each entry in `layers[]` is an uncompressed, unarchived raw file blob:
- **No tar/gzip wrapping:** The OCI layer `digest` (`sha256:<hex>`) and `size` equal the exact file SHA-256 and byte size recorded in `bundle-identity/v1`, CycloneDX BOM `components[type=file]`, and OMS v1.0 `predicate.resources`.
- **Exact relative path:** `annotations["org.opencontainers.image.title"]` must be present on every layer and must equal the normalized POSIX bundle-relative path (validated via `NormalizeAndValidateRelPath`).
- **Strict 1-to-1 correspondence:** No extra layers, no missing layers, no duplicate titles, and no `bundle-manifest.json` in `layers[]`.
- **`config.digest` vs. `manifest_digest`:** The OCI `config.digest` is the SHA-256 of the serialized `bundle-manifest.json` blob bytes; inside that JSON, `manifest_digest` is the RFC 8785 JCS SHA-256 of `BundleManifest` with `manifest_digest: ""` (`internal/manifest.Finalize`). The controller verifies both.

### 3. Three Digest Domains and Verification Bridge

| Digest Domain | Computation | Where Used |
|---|---|---|
| **OCI Manifest Digest** | `sha256(raw OCI model manifest JSON)` | `ModelArtifact.spec.artifact.uri` (`@sha256:...`), OCI Referrer `subject.digest` |
| **Model Bundle Identity (`bundle_id`)** | `sha256(RFC8785_JCS(CanonicalDocument{Files, "bundle-identity/v1"}))` | `ModelArtifact.spec.identity.bundleID`, `BundleManifest.bundle_id`, CycloneDX root hash, `evaluation-evidence/v1` subject, `release-qualification/v1` subject |
| **OMS v1.0 Root Digest** | `sha256(concat(sorted(resources[]. raw_sha256_bytes)))` per OMS v1.0 §6.5.1 | `OMS v1.0` in-toto `subject[0].digest.sha256` |

Required verification bridge executed by the controller:

```text
1. Fetch OCI Model Manifest by immutable @sha256:<oci_manifest_digest>
   └── Verify raw manifest bytes hash to <oci_manifest_digest>

2. Extract layers[] descriptors -> observedFiles []FileEntry{Path, Size, SHA256}
   ├── Recompute bundle_id via verify.ComputeBundleIdentityFromDescriptors(observedFiles)
   │   └── Must equal ModelArtifact.spec.identity.bundleID
   └── Fetch config blob (bundle-manifest.json)
       └── Verify via verify.VerifyBundleManifestBytes(configBytes, observedFiles)

3. Query OCI Referrers API (/v2/<repo>/referrers/sha256:<oci_manifest_digest>)
   └── Every referrer manifest must have subject.digest == sha256:<oci_manifest_digest>
```

### 4. Evidence Artifacts via OCI Referrers

Each evidence artifact is pushed as an OCI Image Manifest v1.1 (`mediaType: application/vnd.oci.image.manifest.v1+json`) with `config` set to the standard OCI empty config (`application/vnd.oci.empty.v1+json`), `subject` pointing to the OCI Model Manifest descriptor, and a deterministic `artifactType`:

| Evidence | OCI Referrer `artifactType` | Layer `mediaType` & `org.opencontainers.image.title` | In-toto `predicateType` / Schema |
|---|---|---|---|
| **OMS v1.0 Signature** | `application/vnd.devenes.oms.signature.v1+json` | Layer 0: `application/vnd.dsse.envelope.v1+json` (`oms.sig.json`) | `https://model_signing/signature/v1.0` |
| **CycloneDX 1.7 BOM** | `application/vnd.devenes.cyclonedx.mlbom.v1+json` | Layer 0: `application/vnd.cyclonedx+json` (`bom.cdx.json`) | `http://cyclonedx.org/schema/bom-1.7.schema.json` |
| **Evaluation Evidence** | `application/vnd.devenes.evaluation.attestation.v1+json` | Layer 0: `application/vnd.dsse.envelope.v1+json` (`evaluation.dsse.json`)<br>Layer 1: `application/vnd.devenes.llm-release-gate.report.v1+json` (`report.json`) | `https://github.com/devenes/ai-model-provenance-gate/attestation/evaluation-evidence/v1` + `llm-release-gate.report/v1` |
| **Release Qualification** | `application/vnd.devenes.release.qualification.v1+json` | Layer 0: `application/vnd.dsse.envelope.v1+json` (`release-qualification.dsse.json`) | `https://github.com/devenes/ai-model-provenance-gate/attestation/release-qualification/v1` |

#### Missing and Duplicate Referrer Resolution Rules
1. **Missing required referrer:** Fails closed (`Verified=False`, reason `MissingRequiredEvidence`).
2. **Multiple referrers of the same `artifactType`:**
   - First, the controller resolves the `release-qualification/v1` referrer. If multiple `release-qualification/v1` referrers exist that verify under `releaseAuthorities`, the controller selects the newest non-expired attestation whose `subject` matches `bundle_id` (or fails closed if two attestations have identical timestamps and conflicting predicates).
   - Second, because the verified `release-qualification/v1` predicate cryptographically pins `oms_verification.signature_digest`, `bom.digest`, `evaluation.attestation_digest`, and `evaluation.report_digest`, any sibling referrer for OMS, BOM, or Evaluation must match the exact digest pinned in the release qualification predicate. Unreferenced or tampered extra referrers cannot substitute for the pinned evidence.
3. **Referrers API vs. OCI Index Fallback:** The primary discovery mechanism is the OCI Distribution v1.1 Referrers API (`/v2/<name>/referrers/<digest>`). If a registry returns `404` on the Referrers endpoint, the OCI v1.1 specification standard fallback tag (`sha256-<64-hex>` OCI Image Index) may only be used if every indexed manifest explicitly carries `subject.digest == sha256:<oci_manifest_digest>` and passes all cryptographic digest-pinning checks. Mutable arbitrary tags (e.g., `:latest`, `:v1.0.0.att`) are strictly forbidden.

## Consequences

**Positive**
- Controller reconstructs `bundle_id` and verifies OMS/BOM file inventories from OCI manifest descriptors without downloading `.safetensors` weights.
- `bundle-manifest.json` in `config` eliminates the self-hashing paradox.
- Dual-layer Evaluation Referrer provides both `evaluation.dsse.json` and `report.json` so all 7 evaluation guarantees are verified in memory.

**Trade-offs**
- One OCI layer descriptor per model file increases manifest JSON size slightly (typically <10 KB for a 20-file model bundle).
- Requires an OCI v1.1-capable client/packager for pushing raw model file layers.

## Acceptance Criteria

- Local OCI 1.1 registry test proves pushing a model bundle + 4 evidence referrers, querying referrers by subject digest, and reconstructing `bundle_id` without fetching weight layer blobs.
- Tampering with any layer digest, title annotation, config blob, or referrer subject digest is deterministically rejected.
