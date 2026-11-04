---
name: Bug Report
about: Report a bug in reconciliation, OCI referrer verification, admission enforcement, Helm chart, or Terraform
title: "[BUG] "
labels: ["bug"]
assignees: ""
---

## Affected Component

- [ ] `ModelArtifactReconciler` / `ClusterModelTrustPolicyReconciler` (`internal/controller`)
- [ ] 7-Stage Verification Engine (`internal/verification`)
- [ ] OCI v1.1 Registry & Referrers Client (`internal/registry`)
- [ ] `ValidatingAdmissionWebhook` (`internal/admission`)
- [ ] CustomResourceDefinitions (`config/crd/`)
- [ ] Helm Chart (`charts/model-admission-controller/`)
- [ ] Terraform Infrastructure (`terraform/`)
- [ ] CLI (`modelctl` / `gke-e2e`)
- [ ] Documentation (`docs/`)

## Expected Behavior

Describe what should happen (e.g., expected admission decision `ALLOW`/`DENY` and structured reason code).

## Actual Behavior

Describe what actually happened and include relevant `ModelArtifact.status` or admission webhook output.

## Steps to Reproduce

1.
2.
3.

## Environment

- Go version:
- GKE cluster version:
- Helm chart version:
