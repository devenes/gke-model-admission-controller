output "project_id" {
  value       = var.project_id
  description = "GCP Project ID"
}

output "cluster_name" {
  value       = google_container_cluster.primary.name
  description = "GKE Standard cluster name"
}

output "cluster_location" {
  value       = google_container_cluster.primary.location
  description = "GKE Standard cluster zone"
}

output "artifact_registry_uri" {
  value       = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.models.repository_id}"
  description = "OCI v1.1 Artifact Registry repository URI"
}

output "controller_gcp_service_account" {
  value       = google_service_account.controller.email
  description = "Controller GCP ServiceAccount email for Workload Identity"
}

output "ci_publisher_gcp_service_account" {
  value       = google_service_account.ci_publisher.email
  description = "CI Publisher GCP ServiceAccount email"
}

output "monitoring_dashboard_id" {
  value       = google_monitoring_dashboard.model_admission.id
  description = "Cloud Monitoring Dashboard ID for GKE Model Admission Controller"
}
