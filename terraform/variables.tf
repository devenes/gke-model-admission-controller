variable "project_id" {
  description = "Google Cloud Project ID"
  type        = string
}

variable "region" {
  description = "Google Cloud region for Artifact Registry and networking"
  type        = string
  default     = "us-west4"
}

variable "zone" {
  description = "Google Cloud zone for the CPU-only GKE Standard cluster"
  type        = string
  default     = "us-west4-a"
}

variable "cluster_name" {
  description = "Name of the GKE Standard cluster"
  type        = string
  default     = "gke-model-admission-demo"
}

variable "node_count" {
  description = "Number of CPU-only nodes in the primary node pool"
  type        = number
  default     = 2
}

variable "machine_type" {
  description = "CPU-only GCE machine type for GKE nodes"
  type        = string
  default     = "e2-standard-4"
}

variable "artifact_registry_repo_id" {
  description = "Artifact Registry repository ID for OCI model bundles, referrers, and controller images"
  type        = string
  default     = "models"
}

variable "controller_namespace" {
  description = "Kubernetes namespace for the GKE Model Admission Controller"
  type        = string
  default     = "model-admission-system"
}

variable "controller_ksa_name" {
  description = "Kubernetes ServiceAccount name for the controller"
  type        = string
  default     = "gke-model-admission-controller"
}
