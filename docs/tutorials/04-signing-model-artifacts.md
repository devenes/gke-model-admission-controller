# Tutorial 04: Signing Model Artifacts with OpenSSF Model Signing v1.0

> **Series Navigation:** [03 Generating a CycloneDX 1.7 AI/ML-BOM](03-generating-ai-ml-bom.md) ← **04** → [05 Running LLM Release Gate Evaluation](05-running-llm-release-gate-evaluation.md)

---

## 1. What you will learn

- How OpenSSF Model Signing (OMS v1.0) signs every file in a model bundle using a DSSE v1.0 (`application/vnd.in-toto+json`) envelope with predicate `https://model_signing/signature/v1.0`.
- How the OMS v1.0 root digest (§6.5.1) is computed and bound to the in-toto `subject[0].digest.sha256`.
- How to sign and verify `/tmp/model-work/oms.sig.json` using the `model-signer` trust domain keypair.

---

## 2. Prerequisites

- Completed [Tutorials 01–03](03-generating-ai-ml-bom.md) with `/tmp/model-work/keys/model-signer.key` and `/tmp/model-work/keys/model-signer.pub` generated.

---

## 3. Architecture context

In `ClusterModelTrustPolicy`, `spec.modelSigners` defines the public keys trusted **exclusively** for OpenSSF Model Signing v1.0 (`oms.sig.json`). When the GKE controller reconciles a `ModelArtifact`, it fetches the `application/vnd.devenes.oms.signature.v1+json` OCI Referrer and calls `pkg/verify.VerifyOMSBytes` with the OCI layer descriptors (`observedFiles`) and `spec.modelSigners`.

---

## 4. Required input files

- [`examples/fixtures/minimal-safetensors/`](../../examples/fixtures/minimal-safetensors/)
- `/tmp/model-work/keys/model-signer.key` (ECDSA P-256 private key)
- `/tmp/model-work/keys/model-signer.pub` (ECDSA P-256 public key)

---

## 5. Step-by-step instructions

1. Run `./bin/ai-model-provenance oms sign` using `--key /tmp/model-work/keys/model-signer.key` and `--key-id model-signer-1` to produce `/tmp/model-work/oms.sig.json`.
2. Run `./bin/ai-model-provenance oms verify` using `--public-key /tmp/model-work/keys/model-signer.pub` to verify the signature and per-file inventory.

---

## 6. Commands

```bash
# 1. Sign the model bundle with OpenSSF Model Signing v1.0
./bin/ai-model-provenance oms sign \
  --bundle ./examples/fixtures/minimal-safetensors \
  --key /tmp/model-work/keys/model-signer.key \
  --key-id model-signer-1 \
  --output /tmp/model-work/oms.sig.json \
  --format text

# 2. Verify the OMS v1.0 signature against the public key and bundle inventory
./bin/ai-model-provenance oms verify \
  --bundle ./examples/fixtures/minimal-safetensors \
  --signature /tmp/model-work/oms.sig.json \
  --public-key /tmp/model-work/keys/model-signer.pub \
  --format text
```

---

## 7. Expected output

```text
STATUS=SIGNED
PREDICATE=https://model_signing/signature/v1.0
ROOT_DIGEST=sha256:6b215206cd8c93d0bf7d0dfabdc008a3cc3870eb204c9c3bbce00d39951f33be
FILES=6
STATUS=VERIFIED
PREDICATE=https://model_signing/signature/v1.0
VERIFIED_FILES=6
REASON=OMS v1.0 signature and file digests verified
```

---

## 8. How to verify success

Decode the DSSE payload inside `/tmp/model-work/oms.sig.json` to inspect the in-toto Statement v1:

```bash
jq -r '.payload' /tmp/model-work/oms.sig.json | base64 -d | jq '{_type, predicateType, subject}'
```

Expected:
- `_type`: `"https://in-toto.io/Statement/v1"`
- `predicateType`: `"https://model_signing/signature/v1.0"`
- `subject[0].digest.sha256`: `"6b215206cd8c93d0bf7d0dfabdc008a3cc3870eb204c9c3bbce00d39951f33be"`

---

## 9. Common errors

| Error | Cause | Fix |
|---|---|---|
| `OMS_SIGNATURE_INVALID` (`N-03`) | Verifying with the wrong public key or after modifying a file in the bundle | Verify with `model-signer.pub` and ensure no files in the bundle changed after signing |
| `UNTRUSTED_SIGNER` (`N-04`) | Signing `oms.sig.json` with `eval-signer.key` or `release-signer.key` | Always sign `oms.sig.json` with a key authorized in `ClusterModelTrustPolicy.spec.modelSigners` |

---

## 10. Security considerations

- **Complete File Coverage (`IgnoreUnsignedFiles: false`)**: Every file in the model bundle must appear in `predicate.resources[]` with its exact SHA-256 digest and byte size. Adding an extra unsigned file to the OCI layers triggers an immediate mismatch.

---

## 11. What comes next

Proceed to **[Tutorial 05: Running Behavioral Evaluation with `llm-release-gate`](05-running-llm-release-gate-evaluation.md)** to execute the behavioral test suite and generate a native `evaluation-evidence/v1` DSSE attestation.
