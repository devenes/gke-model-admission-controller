# Prerequisites

This guide lists the required tools, Google Cloud permissions, and environment variables needed to build, test, and deploy **GKE Model Admission Controller**.

---

## 1. Workstation CLI Tools

| Tool | Minimum Version | Verification Command | Purpose |
|---|---|---|---|
| **Go** | `1.25.0+` | `go version` | Building `controller`, `modelctl`, `gke-e2e`, `ai-model-provenance`, and `llm-release-gate` |
| **Terraform** | `1.6.0+` | `terraform version` | Provisioning Google Cloud VPC, GKE, Artifact Registry, IAM, and Cloud Monitoring |
| **Google Cloud SDK (`gcloud`)** | `470.0.0+` | `gcloud version` | Authenticating to GCP, fetching GKE credentials, and configuring Docker registry auth |
| **`gke-gcloud-auth-plugin`** | Current | `gke-gcloud-auth-plugin --version` | Authenticating `kubectl` and `helm` to the GKE API server |
| **`kubectl`** | `1.29+` | `kubectl version --client` | Inspecting CRDs, applying workloads, and verifying admission webhook behavior |
| **Helm** | `3.14+` | `helm version` | Packaging and installing `charts/model-admission-controller` and `cert-manager` |
| **Docker** | `24.0+` | `docker version` | Building multi-stage distroless `linux/amd64` controller container images |
| **OpenSSL** | `3.0+` | `openssl version` | Generating ECDSA P-256 (`prime256v1`) keypairs for local and demo trust domains |

---

## 2. Sibling Repositories

`gke-model-admission-controller` integrates directly with `ai-model-provenance-gate` (`pkg/verify`) and `llm-release-gate` (`pkg/attestation`). For local development, clone all three repositories into a shared parent directory:

```bash
mkdir -p ~/workspace && cd ~/workspace
git clone https://github.com/devenes/llm-release-gate.git
git clone https://github.com/devenes/ai-model-provenance-gate.git
git clone https://github.com/devenes/gke-model-admission-controller.git
```

---

## 3. Google Cloud Account Requirements

Only required if you are provisioning a live GKE cluster or pushing OCI artifacts to Google Artifact Registry (local unit tests and in-memory OCI registry tests require **zero** cloud credentials):

1. **Billing-enabled Google Cloud Project**:
   - Required APIs (automatically enabled by Terraform in [`terraform/main.tf`](../../terraform/main.tf)):
     - `container.googleapis.com`
     - `artifactregistry.googleapis.com`
     - `compute.googleapis.com`
     - `iam.googleapis.com`
     - `iamcredentials.googleapis.com`
     - `cloudresourcemanager.googleapis.com`
     - `logging.googleapis.com`
     - `monitoring.googleapis.com`
2. **IAM Permissions** for the operator running Terraform:
   - `roles/container.admin`
   - `roles/artifactregistry.admin`
   - `roles/compute.networkAdmin`
   - `roles/iam.serviceAccountAdmin`
   - `roles/resourcemanager.projectIamAdmin`
   - `roles/monitoring.admin`
3. **Compute Quota**:
   - At least `4` CPUs and `16 GB` RAM in your chosen region (`2 x e2-standard-2` CPU-only nodes). No GPU quota is required.

---

## 4. Standard Environment Variables

See [Environment Variables Reference](../ENVIRONMENT.md) for the complete reference. Export these placeholders before running GKE commands:

```bash
export PROJECT_ID="your-gcp-project-id"
export REGION="us-west4"
export ZONE="us-west4-a"
export CLUSTER_NAME="gke-model-admission-demo"
export ARTIFACT_REGISTRY="${REGION}-docker.pkg.dev/${PROJECT_ID}/models"
```
