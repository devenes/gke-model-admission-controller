# Operations & Observability Guide

## 1. Prometheus Metrics (`internal/telemetry/metrics.go`)

The controller exposes Prometheus metrics on `:8080/metrics`, scraped automatically by Google Cloud Managed Service for Prometheus via `PodMonitoring` (`charts/model-admission-controller/templates/podmonitoring.yaml`):

| Metric Name | Type | Labels | Description |
|---|---|---|---|
| `model_admission_requests_total` | Counter | `kind`, `operation` | Total Kubernetes admission requests received |
| `model_admission_decisions_total` | Counter | `decision`, `reason`, `kind`, `mode` | Admission decisions (`allow`, `deny`, `audit_warn`) |
| `model_admission_duration_seconds` | Histogram | `kind` | Admission webhook latency in seconds |
| `model_artifact_verification_total` | Counter | `result`, `reason` | `ModelArtifact` reconciliations by result and reason |
| `model_artifact_verification_duration_seconds` | Histogram | — | Duration of 7-stage OCI supply-chain verification |
| `model_registry_requests_total` | Counter | `operation`, `status` | OCI v1.1 registry HTTP requests (`fetch_manifest`, `fetch_blob`, `list_referrers`) |
| `model_registry_errors_total` | Counter | `operation` | OCI v1.1 registry HTTP/digest errors |
| `model_verification_cache_hits_total` | Counter | — | Admission checks served from verified cache |
| `model_verification_cache_misses_total` | Counter | — | Admission checks encountering unverified/expired state |
| `model_policy_evaluations_total` | Counter | `policy`, `result` | `ClusterModelTrustPolicy` reconciliations |
| `model_verification_failures_total` | Counter | `reason` | Verification failures by structured reason |

---

## 2. Day-2 Operational Procedures

### Rotating Trust Domain Keys

1. Update the `model-trust-keys` `Secret` in `model-admission-system` (or update `publicKeyPEM` / `keyFingerprint` in `ClusterModelTrustPolicy`).
2. `ClusterModelTrustPolicyReconciler` automatically watches `Secret` changes, recomputes `status.policyDigest`, and triggers re-reconciliation of all `ModelArtifact` resources across the cluster.
3. Until re-reconciliation completes, the admission webhook fails closed (`PolicyDigestMismatch`) for any `ModelArtifact` still carrying the old `policyDigest`.

### Switching Between `Audit` and `Enforce` Modes

- Set `spec.enforcementMode: Audit` on `ClusterModelTrustPolicy/default` during initial namespace onboarding to log and emit Kubernetes admission warnings without blocking workloads.
- Set `spec.enforcementMode: Enforce` for strict production enforcement.
