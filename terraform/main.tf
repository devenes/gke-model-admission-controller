locals {
  required_apis = toset([
    "container.googleapis.com",
    "artifactregistry.googleapis.com",
    "iam.googleapis.com",
    "monitoring.googleapis.com",
    "logging.googleapis.com",
    "compute.googleapis.com",
  ])
  master_ipv4_cidr_block = "172.16.0.0/28"
}

resource "google_project_service" "apis" {
  for_each           = local.required_apis
  project            = var.project_id
  service            = each.value
  disable_on_destroy = false
}

# Dedicated VPC and Subnet for the GKE Standard cluster
resource "google_compute_network" "vpc" {
  name                    = "${var.cluster_name}-vpc"
  project                 = var.project_id
  auto_create_subnetworks = false
  depends_on              = [google_project_service.apis]
}

resource "google_compute_subnetwork" "subnet" {
  name                     = "${var.cluster_name}-subnet"
  project                  = var.project_id
  region                   = var.region
  network                  = google_compute_network.vpc.id
  ip_cidr_range            = "10.10.0.0/20"
  private_ip_google_access = true

  secondary_ip_range {
    range_name    = "pods"
    ip_cidr_range = "10.20.0.0/16"
  }

  secondary_ip_range {
    range_name    = "services"
    ip_cidr_range = "10.30.0.0/20"
  }
}

# Cloud Router & Cloud NAT for private GKE worker nodes (no public node IPs required)
resource "google_compute_router" "router" {
  name    = "${var.cluster_name}-router"
  project = var.project_id
  region  = var.region
  network = google_compute_network.vpc.id
}

resource "google_compute_router_nat" "nat" {
  name                               = "${var.cluster_name}-nat"
  project                            = var.project_id
  router                             = google_compute_router.router.name
  region                             = var.region
  nat_ip_allocate_option             = "AUTO_ONLY"
  source_subnetwork_ip_ranges_to_nat = "ALL_SUBNETWORKS_ALL_IP_RANGES"

  log_config {
    enable = true
    filter = "ERRORS_ONLY"
  }
}

# Google Artifact Registry (OCI v1.1 + Referrers API compatible)
resource "google_artifact_registry_repository" "models" {
  project       = var.project_id
  location      = var.region
  repository_id = var.artifact_registry_repo_id
  description   = "OCI v1.1 Model Bundles, OMS Signatures, CycloneDX AI/ML-BOMs, Evaluation & Release Qualification Referrers"
  format        = "DOCKER"
  depends_on    = [google_project_service.apis]
}

# GKE Standard Zonal Cluster (CPU-only, private nodes, Workload Identity enabled, Managed Prometheus enabled)
resource "google_container_cluster" "primary" {
  name                     = var.cluster_name
  project                  = var.project_id
  location                 = var.zone
  remove_default_node_pool = true
  initial_node_count       = 1
  deletion_protection      = false

  network    = google_compute_network.vpc.id
  subnetwork = google_compute_subnetwork.subnet.id

  ip_allocation_policy {
    cluster_secondary_range_name  = "pods"
    services_secondary_range_name = "services"
  }

  private_cluster_config {
    enable_private_nodes    = true
    enable_private_endpoint = false
    master_ipv4_cidr_block  = local.master_ipv4_cidr_block
  }

  workload_identity_config {
    workload_pool = "${var.project_id}.svc.id.goog"
  }

  monitoring_config {
    enable_components = ["SYSTEM_COMPONENTS"]
    managed_prometheus {
      enabled = true
    }
  }

  logging_config {
    enable_components = ["SYSTEM_COMPONENTS", "WORKLOADS"]
  }

  depends_on = [google_project_service.apis]
}

# CPU-only Node Pool (2 x e2-standard-4, no GPUs, private nodes)
resource "google_container_node_pool" "cpu_nodes" {
  name       = "${var.cluster_name}-cpu-pool"
  project    = var.project_id
  location   = var.zone
  cluster    = google_container_cluster.primary.name
  node_count = var.node_count

  node_config {
    machine_type = var.machine_type
    disk_size_gb = 50
    disk_type    = "pd-balanced"
    oauth_scopes = ["https://www.googleapis.com/auth/cloud-platform"]

    workload_metadata_config {
      mode = "GKE_METADATA"
    }

    shielded_instance_config {
      enable_secure_boot          = true
      enable_integrity_monitoring = true
    }

    labels = {
      "workload-type" = "cpu-only"
    }

    tags = ["${var.cluster_name}-node"]
  }
}

# Firewall rule allowing ONLY the GKE Control Plane CIDR and internal cluster CIDRs to reach ValidatingAdmissionWebhook port 9443
resource "google_compute_firewall" "allow_apiserver_webhook_9443" {
  name        = "${var.cluster_name}-allow-webhook-9443"
  project     = var.project_id
  network     = google_compute_network.vpc.name
  direction   = "INGRESS"
  priority    = 1000
  description = "Allow GKE API Server control plane (172.16.0.0/28) and internal cluster CIDRs to invoke ValidatingAdmissionWebhook on TCP port 9443"

  allow {
    protocol = "tcp"
    ports    = ["9443"]
  }

  source_ranges = [
    local.master_ipv4_cidr_block,
    google_compute_subnetwork.subnet.ip_cidr_range,
    "10.20.0.0/16",
  ]
  target_tags = ["${var.cluster_name}-node"]
}

# Least-privilege GCP ServiceAccount for GKE Model Admission Controller (read-only Artifact Registry access)
resource "google_service_account" "controller" {
  account_id   = "gke-model-admission-controller"
  display_name = "GKE Model Admission Controller"
  project      = var.project_id
  depends_on   = [google_project_service.apis]
}

resource "google_artifact_registry_repository_iam_member" "controller_reader" {
  project    = var.project_id
  location   = google_artifact_registry_repository.models.location
  repository = google_artifact_registry_repository.models.name
  role       = "roles/artifactregistry.reader"
  member     = "serviceAccount:${google_service_account.controller.email}"
}

resource "google_service_account_iam_member" "controller_workload_identity" {
  service_account_id = google_service_account.controller.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${var.project_id}.svc.id.goog[${var.controller_namespace}/${var.controller_ksa_name}]"
  depends_on         = [google_container_cluster.primary]
}

# Least-privilege GCP ServiceAccount for CI Qualification & Artifact Publisher
resource "google_service_account" "ci_publisher" {
  account_id   = "model-ci-publisher"
  display_name = "AI Model Qualification CI Publisher"
  project      = var.project_id
  depends_on   = [google_project_service.apis]
}

resource "google_artifact_registry_repository_iam_member" "ci_publisher_writer" {
  project    = var.project_id
  location   = google_artifact_registry_repository.models.location
  repository = google_artifact_registry_repository.models.name
  role       = "roles/artifactregistry.writer"
  member     = "serviceAccount:${google_service_account.ci_publisher.email}"
}

# Cloud Monitoring Dashboard for GKE Model Admission Controller (Managed Prometheus metrics)
resource "google_monitoring_dashboard" "model_admission" {
  project    = var.project_id
  depends_on = [google_project_service.apis]

  dashboard_json = jsonencode({
    displayName = "GKE Model Admission Controller Security & Provenance"
    mosaicLayout = {
      columns = 12
      tiles = [
        {
          width  = 6
          height = 4
          widget = {
            title = "Model Admission Decisions by Outcome"
            xyChart = {
              dataSets = [{
                timeSeriesQuery = {
                  prometheusQuery = "sum by (decision, reason) (rate(model_admission_decisions_total[5m]))"
                }
                plotType = "LINE"
              }]
            }
          }
        },
        {
          xPos   = 6
          width  = 6
          height = 4
          widget = {
            title = "Model Artifact Verification Results"
            xyChart = {
              dataSets = [{
                timeSeriesQuery = {
                  prometheusQuery = "sum by (status, reason) (increase(model_artifact_verification_total[15m]))"
                }
                plotType = "STACKED_BAR"
              }]
            }
          }
        },
        {
          yPos   = 4
          width  = 6
          height = 4
          widget = {
            title = "Admission Webhook Latency (p95)"
            xyChart = {
              dataSets = [{
                timeSeriesQuery = {
                  prometheusQuery = "histogram_quantile(0.95, sum by (le, kind) (rate(model_admission_duration_seconds_bucket[5m])))"
                }
                plotType = "LINE"
              }]
            }
          }
        },
        {
          xPos   = 6
          yPos   = 4
          width  = 6
          height = 4
          widget = {
            title = "OCI Registry Requests & Cache Hit Ratio"
            xyChart = {
              dataSets = [{
                timeSeriesQuery = {
                  prometheusQuery = "sum by (operation, status) (rate(model_registry_requests_total[5m]))"
                }
                plotType = "LINE"
              }]
            }
          }
        }
      ]
    }
  })
}
