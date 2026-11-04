# Cryptographic Key & Policy Rotation Runbook

Rotating public keys in `ClusterModelTrustPolicy` (`modelSigners`, `evaluatorSigners`, or `releaseSigners`) or updating policy constraints (such as `expectedSuiteDigest`, `allowedRuntimeImages`, or `maxAgeHours`) is deterministically tracked via `status.policyDigest`.

---

## 1. Zero-Downtime Signer Key Rotation (Overlap Window Pattern)

If you replace a signer key in a single step before re-signing existing models, those models will immediately transition to `Failed` (`OMS_SIGNATURE_INVALID` / `EVALUATION_INVALID` / `RELEASE_QUALIFICATION_INVALID`).

To rotate keys cleanly without disrupting deployments of currently approved models, use a **3-phase overlap window**:

### Phase 1: Add the New Signer Public Key Alongside the Existing Key

Add the new key (e.g. `model-signer-2`) to `ClusterModelTrustPolicy.spec.modelSigners` while keeping `model-signer-1`:

```yaml
  modelSigners:
    - id: model-signer-1
      name: Previous Production Model Signer
      publicKeyPEM: |
        -----BEGIN PUBLIC KEY-----
        ...
        -----END PUBLIC KEY-----
    - id: model-signer-2
      name: New Production Model Signer (2026-Q4)
      publicKeyPEM: |
        -----BEGIN PUBLIC KEY-----
        ...
        -----END PUBLIC KEY-----
```

When applied:
- `ClusterModelTrustPolicyReconciler` computes a new `status.policyDigest` and increments `metadata.generation`.
- All existing `ModelArtifact` resources are automatically re-queued and re-verify as `Verified` under the new `policyDigest` because `model-signer-1` is still trusted.

### Phase 2: Sign New Model Releases (or Re-Qualify Existing Models) with `model-signer-2`

Switch your CI/CD pipeline (`ai-model-provenance oms sign`) to use `model-signer-2`. Both `model-signer-1` and `model-signer-2` artifacts pass verification during the transition window.

### Phase 3: Decommission `model-signer-1`

Once all active `ModelArtifact` resources in the cluster are signed by `model-signer-2` (`kubectl get modelartifacts -A -o custom-columns=NAME:.metadata.name,SIGNER:.status.omsSigner`), remove `model-signer-1` from `ClusterModelTrustPolicy/default`.

---

## 2. Emergency Key Revocation

If a private key (`model-signer-1`, `eval-signer-1`, or `release-signer-1`) is compromised:

1. **Remove the compromised public key immediately** from `ClusterModelTrustPolicy/default` and `kubectl apply`.
2. Within seconds:
   - `ClusterModelTrustPolicyReconciler` updates `status.policyDigest`.
   - Even before asynchronous `ModelArtifact` reconciliation completes, the synchronous `ValidatingAdmissionWebhook` blocks any new deployment of any `ModelArtifact` whose `status.evaluatedPolicyDigest` matches the old policy (`POLICY_GENERATION_STALE` / `POLICY_DIGEST_MISMATCH`).
   - Asynchronous reconciliation then marks every `ModelArtifact` signed by the revoked key as `status.phase: Failed` (`OMS_SIGNATURE_INVALID` / `EVALUATION_INVALID` / `RELEASE_QUALIFICATION_INVALID`).
