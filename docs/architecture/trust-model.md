# Cryptographic Trust Model

`gke-model-admission-controller` enforces a **three-domain cryptographic trust model** defined in `ClusterModelTrustPolicy.spec.trustDomains`.

---

## 1. The Three Isolated Signer Trust Domains

```yaml
spec:
  trustDomains:
    modelSigners:
      - id: model-signer-1
        keyID: ms-1
        keyFingerprint: sha256:<64-hex>
        secretRef:
          namespace: model-admission-system
          name: model-trust-keys
          key: model-signer.pub
    evaluators:
      - id: eval-signer-1
        keyID: ev-1
        keyFingerprint: sha256:<64-hex>
        secretRef:
          namespace: model-admission-system
          name: model-trust-keys
          key: evaluator.pub
    releaseAuthorities:
      - id: rel-signer-1
        keyID: ra-1
        keyFingerprint: sha256:<64-hex>
        secretRef:
          namespace: model-admission-system
          name: model-trust-keys
          key: release-authority.pub
```

| Trust Domain | Authorized Artifact Type | DSSE Predicate Type | Role in Supply Chain |
|---|---|---|---|
| **`modelSigners`** | `application/vnd.openssf.model-signing.signature.v1+json` | `https://model_signing/signature/v1.0` | Signs the model bundle file inventory (`config.json`, `*.safetensors`, `tokenizer.json`, LoRA files) at build/packaging time. |
| **`evaluators`** | `application/vnd.devenes.evaluation.attestation.v1+json` | `https://devenes.github.io/ai-model-provenance-gate/attestation/evaluation-evidence/v1` | Signs the behavioral evaluation report (`report.json`) and binds it to `bundle_id` at evaluation execution time (`NATIVE_EVALUATOR`). |
| **`releaseAuthorities`** | `application/vnd.devenes.release-qualification.v1+json` | `https://devenes.github.io/ai-model-provenance-gate/attestation/release-qualification/v1` | Verifies all upstream evidence against policy and signs the 5-point release qualification attestation binding `runtime_image` and `serving_config_digest`. |

---

## 2. Why Keys Are Never Interchangeable (Cross-Domain Key Confusion Prevention)

In many CI/CD environments, the evaluation runner (`evaluators`) runs in an environment that has network access to model endpoints and test datasets. If `evaluators` keys were trusted to sign OMS model signatures or Release Qualification attestations, a compromised evaluation runner could package arbitrary model weights, sign them with its evaluator key, and self-approve the release.

To prevent this:

1. [`policy.Resolve`](../../internal/policy/evaluator.go) builds three separate slice lists (`ModelSigners`, `Evaluators`, `ReleaseAuthorities`) plus a combined `AllDomainCandidates` slice.
2. During Stage 4 (OMS verification), Stage 6 (Evaluation verification), and Stage 7 (Release Qualification verification), [`internal/verification/verifier.go`](../../internal/verification/verifier.go) passes `UntrustedCandidates: pol.AllDomainCandidates` to `pkg/verify`.
3. If an OMS signature is signed by a valid `evaluators` or `releaseAuthorities` key, `pkg/verify` detects the cross-domain key usage and rejects the artifact with `UntrustedModelSigner` (verified in live GKE test `N-17`).

---

## 3. Public Key Fingerprint Pinning & Rotation

Each `TrustedPrincipalSpec` supports:

- `secretRef` (referencing a Kubernetes `Secret` key containing a PEM-encoded PKIX Ed25519 public key) or inline `publicKeyPEM`.
- `keyFingerprint`: The deterministic `sha256:<64-lowercase-hex>` digest over the DER-encoded SubjectPublicKeyInfo (SPKI) bytes. If the PEM key inside the Kubernetes `Secret` does not match `keyFingerprint`, `policy.Resolve` fails closed (`ClusterPolicyNotReady`).
- When keys are rotated in `Secret/model-trust-keys` or `ClusterModelTrustPolicy`, `status.policyDigest` changes immediately, invalidating cached `ModelArtifact` verifications until re-verification succeeds with the new keys.
