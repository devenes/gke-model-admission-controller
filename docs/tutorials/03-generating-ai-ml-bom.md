# Tutorial 03: Generating a CycloneDX 1.7 AI/ML-BOM

> **Series Navigation:** [02 Creating a Model Bundle](02-creating-a-model-bundle.md) ← **03** → [04 Signing Model Artifacts (OMS v1.0)](04-signing-model-artifacts.md)

---

## 1. What you will learn

- How to generate a CycloneDX 1.7 AI/ML Bill of Materials (`bom.cdx.json`) bound to the model bundle's `bundle_id` and per-file SHA-256 inventory.
- How `ai-model-provenance bom validate` verifies the CycloneDX 1.7 schema, model card presence, and RFC 8785 JCS canonical hash.

---

## 2. Prerequisites

- Completed [Tutorial 02](02-creating-a-model-bundle.md) with `/tmp/model-work/bundle-manifest.json` created.

---

## 3. Architecture context

In **GKE Model Admission Controller**, `ClusterModelTrustPolicy.spec.bomRequirements` requires every model bundle to carry an OCI Referrer of type `application/vnd.devenes.cyclonedx.mlbom.v1+json` with `minSpecVersion: "1.7"` and `requireModelCard: true`. During reconciliation, `pkg/verify.VerifyBOMBytes` checks every file component in `bom.cdx.json` against the OCI layer descriptors and computes the RFC 8785 JCS canonical digest (`BOMResult.CanonicalHash`), which must match `ReleaseQualificationPredicate.BOM.Digest`.

---

## 4. Required input files

- [`examples/fixtures/minimal-safetensors/`](../../examples/fixtures/minimal-safetensors/)

---

## 5. Step-by-step instructions

1. Run `./bin/ai-model-provenance bom generate` on `./examples/fixtures/minimal-safetensors` to write `/tmp/model-work/bom.cdx.json`.
2. Run `./bin/ai-model-provenance bom validate` against both the bundle directory and `/tmp/model-work/bom.cdx.json`.

---

## 6. Commands

```bash
# 1. Generate CycloneDX 1.7 AI/ML-BOM
./bin/ai-model-provenance bom generate \
  --bundle ./examples/fixtures/minimal-safetensors \
  --output /tmp/model-work/bom.cdx.json \
  --format json

# 2. Validate BOM against the model bundle inventory
./bin/ai-model-provenance bom validate \
  --bundle ./examples/fixtures/minimal-safetensors \
  --bom /tmp/model-work/bom.cdx.json \
  --format text
```

---

## 7. Expected output

```text
STATUS=VERIFIED
SPEC=1.7
BUNDLE_ID=sha256:4ea48db6aa00c7908197674f0818f2c8a09821246ed021c8488ce84873cae97a
BOM_DIGEST=sha256:f8981ca6a2e0d2a8fc5abe4471991149c5f66748bbf4a9efef10f5dfde7400ac
```

---

## 8. How to verify success

Inspect the root component properties in `/tmp/model-work/bom.cdx.json`:

```bash
jq '{specVersion, version, rootName: .metadata.component.name, componentCount: (.components | length)}' /tmp/model-work/bom.cdx.json
```

Expected: `specVersion: "1.7"`, `componentCount: 6`.

---

## 9. Common errors

| Error | Cause | Fix |
|---|---|---|
| `BOM_SPEC_VERSION_UNSUPPORTED` (`N-06`) | CycloneDX BOM generated with `specVersion: "1.5"` or `"1.4"` | Use `ai-model-provenance bom generate` which emits CycloneDX `1.7` |
| `BOM_FILE_HASH_MISMATCH` (`N-07`) | A file in the model directory was modified after `bom.cdx.json` was generated | Re-run `ai-model-provenance inspect` and `ai-model-provenance bom generate` |

---

## 10. Security considerations

- **Deterministic Canonical Digest (`BOM_DIGEST`)**: Because `bom.cdx.json` is canonicalized using RFC 8785 JCS (`sha256:f8981ca6a2e0d2a8fc5abe4471991149c5f66748bbf4a9efef10f5dfde7400ac`), formatting whitespace changes cannot spoof or break digest verification, while any change to component hashes or metadata immediately invalidates the `release-qualification/v1` binding (`N-17`).

---

## 11. What comes next

Proceed to **[Tutorial 04: Signing Model Artifacts with OpenSSF Model Signing v1.0](04-signing-model-artifacts.md)** to sign the model inventory with the `model-signer` ECDSA P-256 key.
