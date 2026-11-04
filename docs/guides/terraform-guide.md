# Terraform Infrastructure Guide

All Google Cloud infrastructure for **GKE Model Admission Controller** is defined declaratively in [`terraform/`](../../terraform/). No Google Cloud resource is created outside Terraform.

---

## 1. File Inventory

| File | Resources Managed |
|---|---|
| [`versions.tf`](../../terraform/versions.tf) | Terraform `>= 1.6.0`, `hashicorp/google >= 5.20, < 7.0`, `hashicorp/google-beta` |
| [`providers.tf`](../../terraform/providers.tf) | Google Cloud provider configuration (`project`, `region`, `zone`) |
| [`variables.tf`](../../terraform/variables.tf) | Input variables (`project_id`, `region`, `zone`, `cluster_name`, `node_count`, `machine_type`, `disk_size_gb`, `disk_type`, `artifact_registry_repository_id`, `controller_namespace`, `controller_ksa_name`) |
| [`main.tf`](../../terraform/main.tf) | `google_project_service` (8 GCP APIs), `google_compute_network.vpc`, `google_compute_subnetwork.subnet`, `google_compute_firewall.gke_master_to_webhook` (`tcp:9443`), `google_artifact_registry_repository.models`, `google_container_cluster.primary`, `google_container_node_pool.cpu_pool` (`2 x e2-standard-2`), `google_service_account` + Workload Identity IAM bindings, and `google_monitoring_dashboard.model_admission` |
| [`outputs.tf`](../../terraform/outputs.tf) | `project_id`, `region`, `zone`, `cluster_name`, `cluster_endpoint`, `artifact_registry_url`, `controller_gsa_email`, `node_gsa_email`, `get_credentials_command` |

---

## 2. Input Variables

| Variable | Default | Description |
|---|---|---|
| `project_id` | *(Required)* | Target Google Cloud Project ID |
| `region` | `"us-west4"` | GCP region for VPC subnet and Artifact Registry |
| `zone` | `"us-west4-a"` | GCP zone for the zonal Standard GKE cluster |
| `cluster_name` | `"gke-model-admission-demo"` | Name of the GKE cluster |
| `node_count` | `2` | Fixed number of CPU-only nodes (`e2-standard-2`) |
| `machine_type` | `"e2-standard-2"` | 2 vCPU, 8 GB RAM per node |
| `disk_size_gb` | `50` | Boot disk size in GB (`pd-balanced`) |
| `artifact_registry_repository_id` | `"models"` | Artifact Registry repository ID |
| `controller_namespace` | `"model-admission-system"` | Kubernetes namespace of the controller |
| `controller_ksa_name` | `"model-admission-controller"` | Kubernetes ServiceAccount name bound via Workload Identity |

---

## 3. Why Standard Zonal CPU-Only GKE?

1. **Zero GPU Cost**: Admission control, OCI descriptor resolution, and ECDSA P-256 verification are purely CPU-bound operations taking `< 25 ms`. Running on `2 x e2-standard-2` nodes costs approximately `$0.134/hr` total for compute while providing multi-replica high availability (`replicas: 2`, `PodDisruptionBudget`).
2. **Explicit Webhook Firewall Control**: Standard GKE allows explicit inspection and management of the control-plane-to-node webhook firewall rule (`tcp:9443` from `172.16.0.0/28`).
3. **Workload Identity Federation**: Binds the Kubernetes ServiceAccount `model-admission-system/model-admission-controller` directly to `gke-model-admission-ctrl@PROJECT_ID.iam.gserviceaccount.com` without static JSON keys.

---

## 4. Common Terraform Commands

```bash
# Format check
terraform -chdir=terraform fmt -check -recursive

# Validate syntax and types
terraform -chdir=terraform validate

# Generate execution plan
terraform -chdir=terraform plan -var-file=environments/demo/terraform.tfvars
```
