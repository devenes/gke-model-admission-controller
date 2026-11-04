# End-to-End Data Flow

This document traces the complete lifecycle of an AI model artifact from local/CI creation to Kubernetes workload admission on GKE.

---

## 1. Ten-Stage End-to-End Data Flow

```mermaid
sequenceDiagram
    autonumber
    participant CI as CI / Producer (ai-model-provenance + llm-release-gate)
    participant GAR as Google Artifact Registry (OCI v1.1)
    participant Rec as ModelArtifactReconciler (GKE)
    participant K8s as Kubernetes API Server (etcd)
    participant Web as ValidatingAdmissionWebhook (:9443)
    participant Ops as ML / Platform Engineer

    CI->>CI: 1. Inspect bundle -> bundle-manifest.json (bundle_id)
    CI->>CI: 2. Sign OMS v1.0 (ms-1) & generate CycloneDX 1.7 BOM
    CI->>CI: 3. Run llm-release-gate -> report.json + evaluation.dsse.json (ev-1)
    CI->>CI: 4. Run gate evaluate -> release-qualification/v1 attestation.json (ra-1)
    CI->>GAR: 5. modelctl push -> Subject Manifest + 4 OCI v1.1 Referrers
    Ops->>K8s: 6. Apply ClusterModelTrustPolicy + ModelArtifact
    K8s->>Rec: 7. Watch event triggers 7-stage reconciliation
    Rec->>GAR: 8. Fetch Subject Manifest, config blob, & 4 Referrers (0 weight bytes)
    Rec->>K8s: 9. Patch ModelArtifact.status (verified=true, policyDigest, validUntil)
    Ops->>K8s: 10. Apply Deployment/Pod (trust.gke.dev/model-artifact)
    K8s->>Web: AdmissionReview (CREATE/UPDATE)
    Web->>K8s: Allow (verified in <50ms from informer cache)
```

---

## 2. What Travels Over the Network at Each Phase

| Phase | Source -> Destination | Payload Transferred | Model Weights Transferred? |
|---|---|---|---|
| **Phase A: OCI Publishing (`modelctl push`)** | CI Runner -> Google Artifact Registry | Model files (`config.json`, `*.safetensors`, `tokenizer.json`), `bundle-manifest.json`, and 4 OCI Referrer artifacts (`oms.sig.json`, `bom.cdx.json`, `evaluation.dsse.json` + `report.json`, `attestation.json`). | Yes (uploaded once to Artifact Registry) |
| **Phase B: Reconciliation (`ModelArtifactReconciler`)** | GKE Controller Pod -> Google Artifact Registry | OCI Subject Manifest JSON, `bundle-manifest.json` config blob, `/v2/<repo>/referrers/<digest>` index JSON, and the 4 referrer manifests/blobs (each capped at `<= 16 MiB`). | **No (`0 bytes` of model weights fetched)** |
| **Phase C: Workload Admission (`ValidatingAdmissionWebhook`)** | Kubernetes API Server -> GKE Controller Pod (`:9443`) | Kubernetes `AdmissionReview` JSON (`Pod`, `Deployment`, `StatefulSet`, `DaemonSet`, `Job`, or `CronJob`). | **No (`0` external network calls)** |
