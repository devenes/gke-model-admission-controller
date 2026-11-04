# Trust Policy Configuration (`ClusterModelTrustPolicy`)

`ClusterModelTrustPolicy` (`trust.gke.dev/v1alpha1`) is the cluster-scoped CustomResourceDefinition that defines the cryptographic trust roots, required evidence types, evaluation freshness windows, and runtime constraints enforced by the controller.

---

## 1. Active Policy Singleton (`default`)

By design, the controller and admission webhook evaluate the cluster-scoped policy named **`default`**. If `ClusterModelTrustPolicy/default` does not exist or is not ready (`status.observedGeneration != metadata.generation`), the admission webhook fails closed (`NO_TRUST_POLICY` / `POLICY_NOT_READY`).

---

## 2. Complete `ClusterModelTrustPolicy` Specification

```yaml
apiVersion: trust.gke.dev/v1alpha1
kind: ClusterModelTrustPolicy
metadata:
  name: default
spec:
  enforcementMode: Enforce
  allowedModelFormats:
    - safetensors
  rejectUnsafePickle: true
  rejectRemoteCodeHooks: true

  modelSigners:
    - id: model-signer-1
      name: Production Model Signer
      publicKeyPEM: |
        -----BEGIN PUBLIC KEY-----
        MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE...
        -----END PUBLIC KEY-----

  evaluatorSigners:
    - id: eval-signer-1
      name: Trusted Evaluation Runner
      role: trusted-evaluator
      publicKeyPEM: |
        -----BEGIN PUBLIC KEY-----
        MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE...
        -----END PUBLIC KEY-----

  releaseSigners:
    - id: release-signer-1
      name: Release Qualification Authority
      publicKeyPEM: |
        -----BEGIN PUBLIC KEY-----
        MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE...
        -----END PUBLIC KEY-----

  bomRequirements:
    required: true
    minSpecVersion: "1.7"
    requireModelCard: true

  evaluationRequirements:
    required: true
    requireTrustedEvaluator: true
    rejectPostHocBinding: true
    maxAgeHours: 720
    expectedSuiteDigest: "sha256:2ef31d0e3121d5359f5e7d02d6ca627e2d3d0e259f06c3ecbf4360e88e0847ca"
    expectedDatasetDigest: "sha256:c83d9a10096a9d2e4bfaaebb8b8e298f724a185c75c4887a81bfdcb4ecd5323a"

  releaseRequirements:
    required: true
    maxAgeHours: 720
    allowedRuntimeImages:
      - "us-west4-docker.pkg.dev/PROJECT_ID/models/vllm-runtime@sha256:*"
    requireRuntimeDigestPinning: true
    requireServingConfigDigestMatch: true

  exemptions:
    allowedServiceAccounts:
      - "system:serviceaccount:ai-serving:breakglass-deployer"
```

---

## 3. Field Reference

| Section | Field | Description |
|---|---|---|
| `spec.enforcementMode` | `Enforce` \| `Audit` | `Enforce` rejects non-compliant workloads at admission; `Audit` allows them while emitting Kubernetes warnings and Prometheus audit metrics |
| `spec.allowedModelFormats` | `[]string` | Permitted model serialization formats (default `["safetensors"]`). Formats outside this list fail with `FORMAT_FORBIDDEN` |
| `spec.rejectUnsafePickle` | `bool` | Reject bundles containing `.pkl`, `.pt`, `.bin`, or pickle opcodes (`UNSAFE_PICKLE_DETECTED`) |
| `spec.rejectRemoteCodeHooks` | `bool` | Reject bundles containing `auto_map` or custom `.py` execution hooks (`REMOTE_CODE_HOOKS_DETECTED`) |
| `spec.modelSigners` | `[]TrustedSigner` | ECDSA P-256 public keys authorized ONLY for OpenSSF Model Signing v1.0 (`oms.sig.json`) |
| `spec.evaluatorSigners` | `[]TrustedSigner` | ECDSA P-256 public keys (`role: trusted-evaluator`) authorized ONLY for `evaluation-evidence/v1` |
| `spec.releaseSigners` | `[]TrustedSigner` | ECDSA P-256 public keys authorized ONLY for `release-qualification/v1` |
| `spec.bomRequirements` | `BOMRequirements` | Requires CycloneDX `1.7` (`minSpecVersion: "1.7"`) |
| `spec.evaluationRequirements` | `EvaluationRequirements` | Enforces `NATIVE_CI_EVALUATOR`, `maxAgeHours`, and optional `expectedSuiteDigest` / `expectedDatasetDigest` |
| `spec.releaseRequirements` | `ReleaseRequirements` | Enforces `maxAgeHours`, `allowedRuntimeImages` patterns, `@sha256:` digest pinning, and serving config digest matching |
| `spec.exemptions.allowedServiceAccounts` | `[]string` | Exact Kubernetes user/ServiceAccount identities allowed to use `trust.gke.dev/exempt: "true"` |

---

## 4. How `status.policyDigest` Works

Whenever `ClusterModelTrustPolicy/default` is reconciled by [`ClusterModelTrustPolicyReconciler`](../../internal/controller/trustpolicy_reconciler.go):
1. All public keys in `modelSigners`, `evaluatorSigners`, and `releaseSigners` are parsed and validated (`verify.ParsePublicKeyPEM`).
2. `policy.ComputePolicyDigest` serializes the policy specification and resolved PKIX key fingerprints using **RFC 8785 JSON Canonicalization Scheme (JCS)** and computes `status.policyDigest` (`sha256:<64-hex>`).
3. Every `ModelArtifact` across all namespaces is automatically re-queued for reconciliation against the new `policyDigest`.
4. Any `ModelArtifact` whose `status.policyGeneration` or `status.evaluatedPolicyDigest` has not yet caught up to the active `ClusterModelTrustPolicy/default` is **blocked immediately** at admission (`POLICY_GENERATION_STALE` / `POLICY_DIGEST_MISMATCH`).
