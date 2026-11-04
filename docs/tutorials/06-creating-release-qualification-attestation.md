# Tutorial 06: Creating the Release Qualification Attestation (`release-qualification/v1`)

> **Series Navigation:** [05 Running LLM Release Gate Evaluation](05-running-llm-release-gate-evaluation.md) ← **06** → [07 Publishing to Artifact Registry](07-publishing-to-artifact-registry.md)

---

## 1. What you will learn

- How `ai-model-provenance gate evaluate` verifies the model bundle, OMS v1.0 signature, CycloneDX 1.7 BOM, and `llm-release-gate` evaluation evidence in one pass.
- How the Release Authority signs `release-qualification.dsse.json` (`release-qualification/v1`), pinning all child artifact digests plus the approved `runtime_image` (`@sha256:...`) and `serving_config_digest`.

---

## 2. Prerequisites

- Completed [Tutorials 01–05](05-running-llm-release-gate-evaluation.md) with all artifacts generated in `/tmp/model-work/`:
  - `bundle-manifest.json`
  - `bom.cdx.json`
  - `oms.sig.json`
  - `report.json`
  - `evaluation.dsse.json`
  - `keys/release-signer.key` and `keys/release-signer.pub`

---

## 3. Architecture context

The **Release Qualification Attestation** (`release-qualification/v1`) is the keystone of the cryptographic trust chain. Signed by `releaseSigners`, its predicate binds:
- `bundle.bundle_id` and `bundle.manifest_digest`
- `bom.digest` (RFC 8785 JCS SHA-256 of `bom.cdx.json`)
- `oms_verification.signature_digest` (raw SHA-256 of `oms.sig.json`)
- `evaluation.report_digest` (normalized SHA-256 of `report.json`) and `evaluation.attestation_digest` (raw SHA-256 of `evaluation.dsse.json`)
- `runtime.runtime_image` (digest-pinned serving container image) and `runtime.serving_config_digest`

Because every child digest is pinned inside `release-qualification.dsse.json`, an attacker cannot swap or transplant individual OCI referrers (`N-17`).

---

## 4. Required input files

- [`examples/fixtures/serving-config.yaml`](../../examples/fixtures/serving-config.yaml) (`serving_config_digest: sha256:c1fb3dd4b72e479221aa59c0b5ff33d4eae61a14c2de7ffdd3a6cf57054f34b8`)
- [`examples/fixtures/policy.yaml`](../../examples/fixtures/policy.yaml) (template referencing signer public keys)
- All `/tmp/model-work/*` files from Tutorials 01–05

---

## 5. Step-by-step instructions

1. Render `/tmp/model-work/policy.yaml` pointing to the public keys in `/tmp/model-work/keys/`.
2. Run `./bin/ai-model-provenance gate evaluate` to verify all upstream artifacts and sign `/tmp/model-work/release-qualification.dsse.json` with `release-signer.key`.
3. Run `./bin/ai-model-provenance attestation verify` to verify the signed release qualification envelope.

---

## 6. Commands

```bash
export PROJECT_ID="${PROJECT_ID:-PROJECT_ID}"
export RUNTIME_IMAGE="us-west4-docker.pkg.dev/${PROJECT_ID}/models/vllm-runtime@sha256:1111111111111111111111111111111111111111111111111111111111111111"

# 1. Create the qualification policy pointing to our 3 domain-separated public keys
cat <<EOF > /tmp/model-work/policy.yaml
schema_version: provenance-gate-policy/v1
name: production-strict-policy
mode: strict
bundle:
  allowed_formats: [safetensors]
  reject_unsafe_pickle: true
  reject_remote_code_hooks: true
  reject_symlinks: true
  allow_unknown_files: false
bom:
  required: true
  allowed_spec_versions: ["1.6", "1.7"]
  require_component_hashes: true
  require_model_card: true
signing:
  required: true
  require_oms_v1: true
  trusted_signers:
    - id: model-signer-1
      name: Production Model Signer
      key_id: model-signer-1
      public_key_path: /tmp/model-work/keys/model-signer.pub
evaluation:
  required: true
  allowed_ statuses: [PASS]
  require_artifact_binding: true
  require_trusted_evaluator_attestation: true
  reject_post_hoc_binding: true
  max_age_hours: 720
  expected_suite_digest: "sha256:2ef31d0e3121d5359f5e7d02d6ca627e2d3d0e259f06c3ecbf4360e88e0847ca"
  expected_dataset_digest: "sha256:c83d9a10096a9d2e4bfaaebb8b8e298f724a185c75c4887a81bfdcb4ecd5323a"
  trusted_evaluators:
    - id: eval-signer-1
      name: Trusted Evaluation Runner
      role: trusted-evaluator
      key_id: eval-signer-1
      public_key_path: /tmp/model-work/keys/eval-signer.pub
release_qualification:
  emit_attestation: true
  max_age_hours: 720
  trusted_authorities:
    - id: release-signer-1
      name: Release Qualification Authority
      key_id: release-signer-1
      public_key_path: /tmp/model-work/keys/release-signer.pub
EOF
sed -i.bak 's/allowed_ statuses/allowed_statuses/' /tmp/model-work/policy.yaml && rm -f /tmp/model-work/policy.yaml.bak

# 2. Evaluate all evidence and sign release-qualification.dsse.json
./bin/ai-model-provenance gate evaluate \
  --bundle ./examples/fixtures/minimal-safetensors \
  --bom /tmp/model-work/bom.cdx.json \
  --signature /tmp/model-work/oms.sig.json \
  --eval-report /tmp/model-work/report.json \
  --eval-attestation /tmp/model-work/evaluation.dsse.json \
  --policy /tmp/model-work/policy.yaml \
  --runtime-image "${RUNTIME_IMAGE}" \
  --serving-config ./examples/fixtures/serving-config.yaml \
  --attestation-key /tmp/model-work/keys/release-signer.key \
  --attestation-key-id release-signer-1 \
  --output-attestation /tmp/model-work/release-qualification.dsse.json \
  --format text

# 3. Verify the generated release qualification attestation
./bin/ai-model-provenance attestation verify \
  --bundle ./examples/fixtures/minimal-safetensors \
  --attestation /tmp/model-work/release-qualification.dsse.json \
  --public-key /tmp/model-work/keys/release-signer.pub \
  --format text
```

---

## 7. Expected output

```text
DECISION=PASS
EXIT_CODE=0
BUNDLE_ID=sha256:4ea48db6aa00c7908197674f0818f2c8a09821246ed021c8488ce84873cae97a
REASONS=All release qualification checks passed
STATUS=VERIFIED
SUBJECT=sha256:4ea48db6aa00c7908197674f0818f2c8a09821246ed021c8488ce84873cae97a
GATE_STATUS=PASS
REASON=Release qualification attestation verified
```

---

## 8. How to verify success

Inspect the pinned child digests inside `/tmp/model-work/release-qualification.dsse.json`:

```bash
jq -r '.payload' /tmp/model-work/release-qualification.dsse.json | base64 -d | jq '.predicate | {schema_version, gate_status, bundle_id: .bundle.bundle_id, manifest_digest: .bundle.manifest_digest, bom_digest: .bom.digest, oms_sig_digest: .oms_verification.signature_digest, eval_report_digest: .evaluation.report_digest, eval_att_digest: .evaluation.attestation_digest, runtime_image: .runtime.runtime_image, serving_config_digest: .runtime.serving_config_digest}'
```

---

## 9. Common errors

| Error | Cause | Fix |
|---|---|---|
| `RELEASE_QUALIFICATION_INVALID` (`N-16`) | Signing `release-qualification.dsse.json` with `model-signer.key` or `eval-signer.key` | Sign with `release-signer.key` |
| `EVIDENCE_CROSS_BINDING_MISMATCH` (`N-17`) | Re-generating `bom.cdx.json` or `oms.sig.json` without re-running `gate evaluate` | Always run `ai-model-provenance gate evaluate` last so `release-qualification.dsse.json` pins the latest child digests |

---

## 10. Security considerations

- **Transplant Prevention**: Because `release-qualification.dsse.json` cryptographically binds all 5 upstream digests (`manifest_digest`, `bom.digest`, `oms_verification.signature_digest`, `evaluation.report_digest`, `evaluation.attestation_digest`), no evidence artifact can be swapped out in the OCI registry without breaking the release authority signature check.

---

## 11. What comes next

Proceed to **[Tutorial 07: Publishing the OCI Model Bundle and 4 Evidence Referrers to Artifact Registry](07-publishing-to-artifact-registry.md)** using `modelctl push`.
