# Troubleshooting Guide

## 1. Common Failure Scenarios & Resolution

### 1.1 `ClusterPolicyNotReady`

- **Symptom**: Workloads are rejected with `[ClusterPolicyNotReady]` or `ModelArtifact` reports `Reason=ClusterPolicyNotReady`.
- **Root Cause**: The `ClusterModelTrustPolicy` singleton (`default`) is missing or references a Kubernetes `Secret` (`secretRef`) that does not exist or lacks the expected PEM key.
- **Resolution**:
  ```bash
  kubectl get clustermodeltrustpolicy default -o yaml
  kubectl -n model-admission-system get secret model-trust-keys
  ```

### 1.2 `UntrustedModelSigner` / `UntrustedEvaluator` / `UntrustedReleaseAuthority`

- **Symptom**: `ModelArtifact` fails verification with `UntrustedModelSigner`, `UntrustedEvaluator`, or `UntrustedReleaseAuthority`.
- **Root Cause**:
  1. The DSSE envelope was signed by a key not listed in the corresponding `trustDomains` section of `ClusterModelTrustPolicy`.
  2. **Cross-Domain Key Confusion**: A key valid in another domain (e.g. `evaluators`) was used to sign an OMS or Release Qualification artifact.
- **Resolution**: Inspect `status.failureMessage` on the `ModelArtifact` and verify key fingerprints (`sha256:<64-hex>` over SPKI DER) against `ClusterModelTrustPolicy.status`.

### 1.3 `PostHocEvaluationRejected`

- **Symptom**: `ModelArtifact` fails with `Reason=PostHocEvaluationRejected`.
- **Root Cause**: The evaluation attestation has `binding_mode: "POST_HOC_OPERATOR"` (`OPERATOR_BINDING_ENDORSEMENT`) while `ClusterModelTrustPolicy.spec.requirements.rejectPostHocBinding` is `true`.
- **Resolution**: Re-run `llm-release-gate run` with `--bundle-manifest`, `--signing-key`, `--signing-key-id`, and `--attestation-output` so the evaluator natively signs `evaluation-evidence/v1` at evaluation time.

### 1.4 Webhook Timeout or `caBundle` Mismatch

- **Symptom**: `Internal error occurred: failed calling webhook "vworkload.trust.gke.dev"`.
- **Root Cause**:
  1. `cert-manager` `Certificate` (`model-admission-controller-webhook-tls`) is not `Ready` yet, or `cert-manager-cainjector` has not populated `caBundle`.
  2. Firewall rule `gke-model-admission-demo-allow-webhook-9443` does not cover the GKE control plane CIDR (`172.16.0.0/28`).
- **Resolution**:
  ```bash
  kubectl -n model-admission-system get certificate,secret
  kubectl get validatingwebhookconfiguration model-admission-controller -o jsonpath='{.webhooks[0].clientConfig.caBundle}' | wc -c
  ```
