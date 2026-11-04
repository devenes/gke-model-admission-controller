# Concept: Release Qualification & 5-Point Cross-Evidence Binding

The **Release Qualification Attestation** (`release-qualification/v1`) is the capstone cryptographic artifact in the supply chain. Signed by `trustDomains.releaseAuthorities`, it proves that all upstream artifacts were verified together under a strict release policy and binds the model bundle to its approved deployment composition.

---

## 1. Five-Point Cross-Evidence Digest Binding

An attacker might attempt a **mix-and-match attack** (`TM-08` / `N-23`): taking a valid OMS signature from Build 1, a CycloneDX BOM from Build 2, and an evaluation report from Build 3.

Stage 7 of [`internal/verification/verifier.go`](../../internal/verification/verifier.go) prevents this by verifying that the `release-qualification/v1` predicate signed by `trustDomains.releaseAuthorities` contains the **exact SHA-256 digests** of all 5 upstream artifacts observed in the OCI registry:

| Predicate Field in `release-qualification/v1` | Verified Against Observed Artifact in Stage 7 | Failure Reason on Mismatch |
|---|---|---|
| `subject[0].digest.sha256` | `bm.BundleID` (`bundle-identity/v1`) | `CrossEvidenceMismatch` |
| `predicate.bundle.manifest_digest` | `bm.ManifestDigest` (`bundle-manifest.json` JCS digest) | `CrossEvidenceMismatch` |
| `predicate.bom.digest` | `bomRes.CanonicalHash` (CycloneDX 1.7 JCS digest) | `CrossEvidenceMismatch` |
| `predicate.oms_verification.signature_digest` | `omsRes.SignatureDigest` (`oms.sig.json` SHA-256) | `CrossEvidenceMismatch` |
| `predicate.evaluation.report_digest` & `attestation_digest` | `evalRes.ReportDigest` & `evalRes.AttestationDigest` | `CrossEvidenceMismatch` |

---

## 2. Deployment Composition Binding (`runtime_image` & `serving_config_digest`)

In addition to the 5 upstream evidence digests, `release-qualification/v1` binds the runtime serving environment:

- **`predicate.deployment.runtime_image`**: The immutable `@sha256:<64-hex>` URI of the approved serving container image. Verified against `ModelArtifact.spec.runtime.runtimeImage` during reconciliation and against `PodSpec.containers[*].image` at admission time (`RuntimeImageMismatch`, `N-25`).
- **`predicate.deployment.serving_config_digest`**: The `sha256:<64-hex>` digest of the serving configuration. Verified against `ModelArtifact.spec.runtime.servingConfigDigest` during reconciliation and against `annotations["trust.gke.dev/serving-config-digest"]` at admission time (`ServingConfigDigestMismatch`, `N-26`).
