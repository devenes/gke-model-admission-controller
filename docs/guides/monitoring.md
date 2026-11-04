# Monitoring, Prometheus Metrics & Cloud Monitoring Guide

**GKE Model Admission Controller** exposes 11 Prometheus metrics on `:8080/metrics` (`internal/metrics/metrics.go`) and provisions a Google Cloud Monitoring dashboard via Terraform (`terraform/monitoring.tf`).

---

## 1. Complete Prometheus Metrics Catalog (`internal/metrics/metrics.go`)

All metrics are registered with the `controller-runtime` Prometheus registry (`sigs.k8s.io/controller-runtime/pkg/metrics.Registry`).

| # | Metric Name | Type | Labels | Description |
|---|---|---|---|---|
| 1 | `model_admission_decisions_total` | `CounterVec` | `namespace`, `kind`, `decision`, `mode`, `reason` | Total Kubernetes workload admission decisions (`allow`, `deny`, `audit_warn`) by workload kind and reason code |
| 2 | `model_admission_latency_seconds` | `HistogramVec` | `kind`, `decision` | Latency distribution of synchronous `ValidatingAdmissionWebhook` evaluations in seconds |
| 3 | `model_artifact_reconcile_total` | `CounterVec` | `namespace`, `name`, `result`, `reason` | Total `ModelArtifact` reconciliations (`verified`, `failed`) with specific cryptographic or policy failure codes |
| 4 | `model_artifact_reconcile_duration_seconds` | `HistogramVec` | `result` | End-to-end duration of `ModelArtifact` OCI resolution and 7-step cryptographic verification |
| 5 | `model_artifact_verification_status` | `GaugeVec` | `namespace`, `name`, `phase` | Current verification status (`1` for active `phase` in `Pending`, `Verified`, `Failed`, `0` otherwise) |
| 6 | `oci_referrer_lookup_total` | `CounterVec` | `result`, `artifact_type` | Total OCI v1.1 Referrers resolutions (`success`, `error`, `found`) by evidence `artifactType` |
| 7 | `oci_referrer_lookup_duration_seconds` | `HistogramVec` | `result` | Latency of OCI manifest and 4-referrer metadata resolution from Artifact Registry |
| 8 | `oms_verification_total` | `CounterVec` | `result`, `reason` | Total OpenSSF Model Signing v1.0 (`oms.sig.json`) cryptographic checks |
| 9 | `bom_verification_total` | `CounterVec` | `result`, `reason` | Total CycloneDX 1.7 AI/ML-BOM (`bom.cdx.json`) structural and hash inventory checks |
| 10 | `evaluation_verification_total` | `CounterVec` | `result`, `reason` | Total `llm-release-gate` report and `evaluation-evidence/v1` DSSE checks |
| 11 | `release_qualification_verification_total` | `CounterVec` | `result`, `reason` | Total `release-qualification/v1` DSSE and cross-evidence digest binding checks |

---

## 2. Inspecting Metrics Live from the Cluster

Port-forward or `kubectl exec` to query the metrics endpoint (or scrape via `Service/model-admission-controller-metrics` on port `8080`):

```bash
kubectl run metrics-check -n model-admission-system --rm -i --restart=Never \
  --image=curlimages/curl:8.10.1 -- \
  curl -s http://model-admission-controller-metrics.model-admission-system.svc.cluster.local:8080/metrics \
  | grep -E "^(model_admission_|model_artifact_|oci_referrer_|oms_verification_|bom_verification_|evaluation_verification_|release_qualification_)"
```

Example live output from our GKE verification run:

```prometheus
bom_verification_total{reason="VERIFIED",result="VERIFIED"} 5
evaluation_verification_total{reason="NATIVE_TRUSTED_EVALUATION_VERIFIED",result="VERIFIED"} 5
model_admission_decisions_total{decision="allow",kind="Deployment",mode="Enforce",namespace="ai-serving",reason="VERIFIED"} 18
model_admission_decisions_total{decision="deny",kind="Deployment",mode="Enforce",namespace="ai-serving",reason="MISSING_MODEL_ARTIFACT_REF"} 4
model_admission_decisions_total{decision="deny",kind="Deployment",mode="Enforce",namespace="ai-serving",reason="MODEL_ARTIFACT_NOT_VERIFIED"} 52
model_admission_decisions_total{decision="deny",kind="Deployment",mode="Enforce",namespace="ai-serving",reason="UNAUTHORIZED_EXEMPTION"} 4
model_artifact_reconcile_total{name="gemma-2b-it",namespace="ai-serving",reason="VERIFIED",result="verified"} 4
model_artifact_reconcile_total{name="gemma-2b-lora",namespace="ai-serving",reason="VERIFIED",result="verified"} 4
oci_referrer_lookup_total{artifact_type="all",result="success"} 56
oms_verification_total{reason="VERIFIED",result="VERIFIED"} 5
release_qualification_verification_total{reason="VERIFIED",result="VERIFIED"} 5
```

---

## 3. Useful PromQL Queries

### Admission Denial Rate by Reason Code (5-Minute Window)
```promql
sum by (reason, kind) (rate(model_admission_decisions_total{decision="deny"}[5m]))
```

### 99th Percentile Webhook Admission Latency
```promql
histogram_quantile(0.99, sum by (le, decision) (rate(model_admission_latency_seconds_bucket[5m])))
```

### Unverified (`Failed`) ModelArtifacts Currently Registered
```promql
model_artifact_verification_status{phase="Failed"} == 1
```

### OCI Referrer Resolution Latency (95th Percentile)
```promql
histogram_quantile(0.95, sum by (le) (rate(oci_referrer_lookup_duration_seconds_bucket[5m])))
```

---

## 4. Google Cloud Monitoring Dashboard (`terraform/monitoring.tf`)

[`terraform/main.tf`](../../terraform/main.tf) provisions a `google_monitoring_dashboard` named **GKE Model Admission Controller — Security & Operations** with 6 tiles:

1. **Controller Pod CPU Usage** (`kubernetes.io/container/cpu/core_usage_time` in `model-admission-system`)
2. **Controller Pod Memory Usage** (`kubernetes.io/container/memory/used_bytes` in `model-admission-system`)
3. **Controller Container Restart Count** (`kubernetes.io/container/restart_count` in `model-admission-system`)
4. **Admission Decisions by Outcome & Reason (GMP)** (`prometheus.googleapis.com/model_admission_decisions_total/counter`)
5. **ModelArtifact Reconciliation Rate (GMP)** (`prometheus.googleapis.com/model_artifact_reconcile_total/counter`)
6. **Controller & Webhook Audit Logs Panel** (`resource.type="k8s_container" AND resource.labels.namespace_name="model-admission-system"`)
