# Technology Stack & Component Rationale

This document details the technology stack used across **`gke-model-admission-controller`**, **`ai-model-provenance-gate`**, and **`llm-release-gate`**, including exact module versions from [`go.mod`](../go.mod), [`terraform/versions.tf`](../terraform/versions.tf), and [`charts/model-admission-controller/Chart.yaml`](../charts/model-admission-controller/Chart.yaml), why each component was chosen, and its architectural trade-offs.

---

## 1. Stack Overview Table

| Layer | Technology | Version / Spec | Why It Was Selected |
|---|---|---|---|
| **Language & Runtime** | Go (`CGO_ENABLED=0`) | `go 1.27.3` (CI `1.24+`) | Static compilation into small, rootless binaries with first-class Kubernetes API and cryptographic standard library (`crypto/ed25519`, `crypto/sha256`, `crypto/x509`) support. |
| **Kubernetes Operator Framework** | `sigs.k8s.io/controller-runtime` | `v0.25.2` (`k8s.io/api v0.37.0`) | Provides informer caches, leader-elected controllers (`ClusterModelTrustPolicyReconciler`, `ModelArtifactReconciler`), and TLS `ValidatingAdmissionWebhook` serving in a single process. |
| **Declarative Kubernetes API** | CustomResourceDefinitions (CRDs) | `trust.gke.dev/v1alpha1` (`apiextensions.k8s.io/v1`) | Separates asynchronous registry verification (`ModelArtifact`) and cluster trust configuration (`ClusterModelTrustPolicy`) from synchronous workload admission (`Pod`, `Deployment`, etc.). Includes CEL immutability rules (`x-kubernetes-validations`). |
| **Admission Enforcement** | `ValidatingAdmissionWebhook` | `admissionregistration.k8s.io/v1` (`failurePolicy: Fail`) | Intercepts `CREATE` and `UPDATE` operations on `Pod`, `Deployment`, `StatefulSet`, `DaemonSet`, `Job`, and `CronJob` in enforced namespaces (`trust.gke.dev/enforce: "true"`). |
| **Verification Library** | `github.com/devenes/ai-model-provenance-gate/pkg/verify` | `v0.2.0` | Pure in-memory, byte-slice verification API ([ADR-001](adr/ADR-001-verifier-api.md)) shared between CLI tools and the GKE controller without spawning subprocesses. |
| **Model Bundle Identity** | `bundle-identity/v1` + RFC 8785 (JCS) | `bundle-identity/v1` | Computes a deterministic `sha256:<64-hex>` content identity over sorted model files (`path`, `sha256`, `size`, `role`) independent of registry transport metadata. |
| **Model Signing** | OpenSSF Model Signing (OMS) | `https://model_signing/signature/v1.0` | Verifies Ed25519 DSSE / Sigstore v0.3 signatures over per-file SHA-256 resource digests and OMS root digest (`trustDomains.modelSigners`). |
| **AI/ML Bill of Materials** | CycloneDX (`github.com/CycloneDX/cyclonedx-go`) | Spec `1.7` (`v0.12.0` Go SDK) | Standardizes AI/ML component inventories (model configs, weight shards, tokenizers, and LoRA base model dependencies) with 1:1 file hash verification. |
| **Behavioral Evaluation** | `github.com/devenes/llm-release-gate` | `llm-release-gate.report/v1` + `evaluation-evidence/v1` | Evaluates model behavior against test suites and datasets, emitting native DSSE attestations (`NATIVE_EVALUATOR`) bound to the model `bundle_id` at evaluation time ([ADR-003](adr/ADR-003-release-attestation.md)). |
| **Release Qualification** | in-toto Statement v1 + DSSE | `release-qualification/v1` (`https://in-toto.io/Statement/v1`) | Cryptographically binds all 5 supply-chain digests (`manifest_digest`, `bom.digest`, `oms.signature_digest`, `eval.report_digest`, `eval.attestation_digest`) plus `runtime_image` and `serving_config_digest` under `trustDomains.releaseAuthorities`. |
| **Artifact Distribution** | OCI Distribution Specification + Referrers API | OCI `v1.1` | Stores the model bundle as an OCI Subject Manifest (`application/vnd.devenes.model-bundle.v1`) and attaches all 4 evidence artifacts via `subject.digest`, discoverable via `GET /v2/<repo>/referrers/<digest>` ([ADR-002](adr/ADR-002-oci-model-artifact.md)). |
| **Cloud Registry** | Google Artifact Registry | `DOCKER` / OCI v1.1 | Native support for OCI v1.1 manifests, `subject` descriptors, `/referrers/` index discovery, and keyless GKE Workload Identity authentication. |
| **Cloud Infrastructure (IaC)** | Terraform (`hashicorp/google`) | Terraform `>= 1.5.0`, Provider `~> 6.0` | Declaratively manages all 20 GCP resources (APIs, VPC, Subnet, Cloud Router, Cloud NAT, Firewall, GKE Standard cluster, CPU node pool, Artifact Registry, GSAs, IAM bindings, and Cloud Monitoring Dashboard). |
| **Workload Authentication** | GKE Workload Identity Federation | `PROJECT_ID.svc.id.goog` | Binds the controller Kubernetes ServiceAccount to a least-privilege Google Service Account (`roles/artifactregistry.reader`) via short-lived OAuth2 tokens from the GKE metadata server—zero static JSON keys. |
| **Kubernetes Packaging** | Helm | Chart `0.1.0` (App `v0.1.0`) | Packages the 2-replica controller Deployment, `PodDisruptionBudget`, RBAC, Service, `cert-manager` `Issuer`/`Certificate`, `ValidatingWebhookConfiguration`, and `PodMonitoring`. |
| **Webhook TLS Lifecycle** | `cert-manager` | `v1.21.x` (`cert-manager.io/v1`) | Automates X.509 certificate issuance, rotation, and `caBundle` injection into the `ValidatingWebhookConfiguration`. |
| **Observability & Metrics** | Prometheus (`github.com/prometheus/client_golang`) + GMP | `v1.24.0` + `monitoring.googleapis.com/v1` | Exposes 11 low-cardinality Prometheus metrics on `:8080/metrics`, scraped by Google Cloud Managed Service for Prometheus (`PodMonitoring`) and visualized in Cloud Monitoring. |

---

## 2. How the Components Integrate

1. **Offline / CI Producer Layer (`llm-release-gate` + `ai-model-provenance-gate` + `modelctl`)**:
   - `ai-model-provenance` inspects the model directory, produces `bundle-manifest.json` (`bundle-identity/v1`), signs the OMS v1.0 DSSE signature (`oms.sig.json`), and generates the CycloneDX 1.7 AI/ML-BOM (`bom.cdx.json`).
   - `llm-release-gate run` executes the evaluation suite against the candidate model while reading `--bundle-manifest` and `--signing-key` to emit both `report.json` (`llm-release-gate.report/v1`) and a native DSSE evaluation attestation (`evaluation.dsse.json`, `binding_mode: "NATIVE_EVALUATOR"`).
   - `ai-model-provenance gate evaluate` (or the `pkg/verify` API in `modelctl`) verifies all upstream evidence and signs the `release-qualification/v1` DSSE attestation binding all five digests plus the runtime container image digest and serving config digest.
   - `modelctl push` uploads the model bundle and all four evidence referrers to Google Artifact Registry using OCI Distribution v1.1.
2. **Asynchronous Reconciliation Layer (`ModelArtifactReconciler` + `ClusterModelTrustPolicyReconciler`)**:
   - Runs inside the GKE cluster in namespace `model-admission-system`.
   - Authenticates to Google Artifact Registry via Workload Identity Federation.
   - Fetches only the OCI Subject Manifest, `bundle-manifest.json` config blob, and the 4 referrer manifests/blobs (`<= 16 MiB` ceiling)—**never downloading model weight blobs**.
   - Executes the 7-stage verification pipeline in memory via `ai-model-provenance-gate/pkg/verify` and writes `status.verified`, `status.policyDigest`, and `status.validUntil` onto the `ModelArtifact` resource.
3. **Synchronous Enforcement Layer (`WorkloadValidator` `ValidatingAdmissionWebhook`)**:
   - Intercepts Kubernetes workload creations/updates in `<50ms` with **zero outbound network calls**, reading only the reconciled `ModelArtifact.status` and `PolicyStore`.

---

## 3. Architectural Trade-Offs & Limitations

- **Pre-Registration Requirement (`ModelArtifact` CRD)**: Workloads cannot reference arbitrary registry URIs inline in Pod annotations without a corresponding `ModelArtifact` custom resource in the same namespace. This is an intentional trade-off: pre-registration enables asynchronous OCI referrer verification and keeps admission webhook latency deterministic (`<50ms`) even when the registry is slow or transiently unavailable.
- **Ed25519 Key Material Management**: In v0.1.0, trusted public keys are configured inline (`publicKeyPEM`) or via Kubernetes `Secret` references (`secretRef`) in `ClusterModelTrustPolicy` with SHA-256 SPKI fingerprints (`keyFingerprint`), rather than online Fulcio/Rekor transparency log lookups during reconciliation. This eliminates external internet dependencies from private GKE nodes.
- **Descriptor-Level Integrity for Weight Blobs**: Because the controller verifies weight layers via OCI manifest descriptors (`layers[].digest` and `layers[].size`) against signed `bundle-manifest.json`, OMS v1.0, and CycloneDX 1.7 BOM hashes without downloading multi-gigabyte weight blobs, it relies on the OCI registry (Google Artifact Registry) enforcing content-addressable blob digest integrity on upload and pull.
