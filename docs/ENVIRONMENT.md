# Environment & Configuration Reference (`ENVIRONMENT.md`)

This document describes the placeholders used throughout the documentation, Makefile variables, Terraform variables, and runtime credential resolution.

---

## 1. Documentation Placeholders

All public documentation and example manifests use the following placeholders instead of environment-specific identifiers:

| Placeholder | Example Value | Description |
|---|---|---|
| `PROJECT_ID` | `my-gcp-project` | Google Cloud Project ID where GKE and Artifact Registry are provisioned. |
| `REGION` | `us-west4` | Google Cloud region hosting the VPC subnet, Cloud NAT, and Artifact Registry. |
| `ZONE` | `us-west4-a` | Google Cloud zone hosting the CPU-only GKE Standard cluster (`2 x e2-standard-4`). |
| `ARTIFACT_REGISTRY` | `us-west4-docker.pkg.dev/PROJECT_ID/models` | OCI Distribution v1.1 repository prefix in Google Artifact Registry. |
| `CLUSTER_NAME` | `gke-model-admission-demo` | Name of the GKE Standard cluster. |

---

## 2. Makefile Variables

You can override any variable in [`Makefile`](../Makefile) via the environment or CLI arguments:

```bash
make test-e2e-gke \
  PROJECT_ID=my-gcp-project \
  REGION=us-west4 \
  ZONE=us-west4-a \
  CLUSTER_NAME=gke-model-admission-demo \
  RUNTIME_IMAGE="us-west4-docker.pkg.dev/my-gcp-project/models/gke-model-admission-controller@sha256:9ca52f2367a58106b6081085e6d3c9020409e03dbc34e21155f06600e8d83c93"
```

---

## 3. Terraform Input Variables ([`terraform/variables.tf`](../terraform/variables.tf))

Copy [`terraform/environments/demo/terraform.tfvars.example`](../terraform/environments/demo/terraform.tfvars.example) to `terraform/environments/demo/terraform.tfvars` (which is git-ignored so your personal project ID is never committed):

| Variable | Type | Default | Description |
|---|---|---|---|
| `project_id` | `string` | *(required)* | Google Cloud Project ID. |
| `region` | `string` | `"us-west4"` | GCP region for Artifact Registry and networking. |
| `zone` | `string` | `"us-west4-a"` | GCP zone for the CPU-only GKE Standard cluster. |
| `cluster_name` | `string` | `"gke-model-admission-demo"` | Name of the GKE Standard cluster. |
| `node_count` | `number` | `2` | Number of CPU-only worker nodes in the primary node pool. |
| `machine_type` | `string` | `"e2-standard-4"` | CPU-only Compute Engine machine type (`4 vCPU, 16 GB RAM`). |
| `artifact_registry_repo_id` | `string` | `"models"` | Artifact Registry repository ID. |
| `controller_namespace` | `string` | `"model-admission-system"` | Kubernetes namespace for the controller. |
| `controller_ksa_name` | `string` | `"gke-model-admission-controller"` | Kubernetes ServiceAccount name bound via Workload Identity. |

---

## 4. Credential & Token Resolution ([`internal/registry/gcp_auth.go`](../internal/registry/gcp_auth.go))

### Inside GKE (`manager` Pod)

The controller uses [`GCPMetadataTokenProvider`](../internal/registry/gcp_auth.go) to fetch short-lived OAuth2 access tokens from the GKE Workload Identity metadata server:

- Endpoint: `http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token`
- Header: `Metadata-Flavor: Google`
- Bound Google Service Account role: `roles/artifactregistry.reader`
- **No static service account JSON keys or Kubernetes imagePullSecrets are used.**

### Local CLI (`modelctl` and `gke-e2e`)

When running `modelctl` or `gke-e2e` on a developer workstation outside GKE, `GCPMetadataTokenProvider` checks:

1. `GCP_ACCESS_TOKEN` environment variable (if set).
2. The GCE metadata server (if running on a GCE VM / GKE Pod).
3. When `GcloudTokenFallback = true` (enabled in `modelctl` and `gke-e2e`), falls back to `gcloud auth print-access-token`.
4. For `kubectl` / `client-go` operations in `gke-e2e`, uses standard `KUBECONFIG` or `~/.kube/config`.
