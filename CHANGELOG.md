# Changelog

All notable changes to `gke-model-admission-controller` are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-11-04

### Added

- **Kubernetes API (`trust.gke.dev/v1alpha1`)**:
  - `ClusterModelTrustPolicy` cluster-scoped CRD defining `allowedRegistries`, three isolated cryptographic trust domains (`modelSigners`, `evaluators`, `releaseAuthorities`), verification requirements, `verificationTTLSeconds`, `maxEvidenceAgeHours`, and policy-hashed exemptions.
  - `ModelArtifact` namespaced CRD defining immutable OCI Model Bundle coordinates (`@sha256:<64-hex>`), expected `bundle-identity/v1` digest, runtime container image and serving configuration bindings, optional LoRA `baseModelArtifact` reference, and 7-condition verification status.
- **OCI Distribution v1.1 & Referrers Verification Engine (`internal/registry`, `internal/verification`)**:
  - Zero-weight-download OCI v1.1 client with Google Cloud Workload Identity Federation token acquisition and hard `16 MiB` metadata blob ceiling (`MaxMetadataBlobBytes`).
  - 7-stage supply-chain verification pipeline bridging OCI manifest digests, `bundle-identity/v1`, OpenSSF Model Signing (OMS v1.0), CycloneDX 1.7 AI/ML-BOM, native `llm-release-gate` DSSE evaluation attestations (`evaluation-evidence/v1`), and `release-qualification/v1` 5-point cross-evidence bindings via [`ai-model-provenance-gate/pkg/verify`](https://github.com/devenes/ai-model-provenance-gate).
- **Zero-Network-I/O Validating Admission Webhook (`internal/admission`)**:
  - Synchronous enforcement across `Pod`, `Deployment`, `StatefulSet`, `DaemonSet`, `Job`, and `CronJob` resources in namespaces labeled `trust.gke.dev/enforce: "true"`.
  - Fail-closed (`failurePolicy: Fail`) validation against cached `ModelArtifact.status`, `ClusterModelTrustPolicy.status`, and `PolicyStore`.
  - Non-self-assignable break-glass exemption evaluation bound to the Kubernetes API-server-authenticated requester (`req.UserInfo.Username`) and required Pod labels.
- **Observability (`internal/telemetry`)**:
  - 11 Prometheus metrics for admission requests, decisions, latency, artifact verification, OCI registry requests/errors, cache hits/misses, policy evaluations, and failure reasons.
  - Google Cloud Managed Service for Prometheus `PodMonitoring` and Cloud Monitoring dashboard definition.
- **Infrastructure & Packaging (`terraform/`, `charts/model-admission-controller/`, `cmd/`)**:
  - Terraform configuration for CPU-only zonal GKE Standard cluster, private worker nodes, Cloud NAT, scoped port `9443` webhook firewall, Google Artifact Registry, and Workload Identity IAM bindings.
  - High-availability 2-replica Helm chart with `PodDisruptionBudget`, `cert-manager` TLS lifecycle, and hardened distroless security context.
  - `modelctl` OCI v1.1 Model Bundle + 4 Referrers publisher CLI and `gke-e2e` live GKE verification runner (10 positive, 28 negative security, and 4 operational resilience scenarios).
