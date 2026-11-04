# Contributing to GKE Model Admission Controller

Thank you for your interest in contributing to **GKE Model Admission Controller** (`trust.gke.dev/v1alpha1`). This document explains the development workflow, architectural invariants, testing requirements, and pull request process.

---

## 1. Architectural Invariants (Non-Negotiable)

Every contribution must preserve the core security invariants of the project:

1. **Zero Network/Registry I/O on the Admission Path**: The `ValidatingAdmissionWebhook` ([`internal/admission/validator.go`](internal/admission/validator.go)) must never perform outbound HTTP calls to OCI registries or external services. All registry I/O belongs in [`ModelArtifactReconciler`](internal/controller/modelartifact_reconciler.go).
2. **Zero Model Weight Downloads**: The controller must never download model weight layers (`*.safetensors`). All metadata and referrer reads must go through [`Client.FetchMetadataBlob`](internal/registry/client.go), which enforces `MaxMetadataBlobBytes = 16 MiB`.
3. **Fail-Closed Admission (`failurePolicy: Fail`)**: Missing annotations, unverified `ModelArtifact` resources, stale `observedGeneration`, mismatched `policyDigest`, or expired `status.validUntil` must be rejected in `Enforce` mode.
4. **Three Isolated Trust Domains**: `modelSigners`, `evaluators`, and `releaseAuthorities` keys must remain strictly isolated. Never allow a key from one trust domain to satisfy verification in another domain.
5. **No Subprocess Verification**: Cryptographic and schema verification must use the pure in-memory Go API [`github.com/devenes/ai-model-provenance-gate/pkg/verify`](https://github.com/devenes/ai-model-provenance-gate) rather than shelling out to CLI binaries.
6. **Non-Self-Assignable Exemptions**: Admission exemptions must validate the Kubernetes API-server-authenticated requester (`req.UserInfo.Username`), never the user-editable `pod.spec.serviceAccountName` field.

---

## 2. Local Development Prerequisites

- **Go**: `1.24+` (tested with `go1.27.3`)
- **Helm**: `v3.14+`
- **Terraform**: `v1.6+`
- **kubectl** and **gcloud CLI** (for live GKE integration and E2E testing)
- Sibling repositories cloned alongside `gke-model-admission-controller` when developing cross-repository changes:
  ```text
  parent-directory/
  ├── llm-release-gate/
  ├── ai-model-provenance-gate/
  └── gke-model-admission-controller/
  ```

> [!NOTE]
> **Why Kind and envtest are not used**: This project validates Go unit tests, policy digest determinism, OCI v1.1 referrer parsing, and the 28-scenario security matrix locally via `go test -race ./...`, while all Kubernetes API, CRD CEL validation, `cert-manager` TLS, Workload Identity, Google Artifact Registry Referrers API, and admission webhook E2E tests run against a real **GKE Standard** cluster.

---

## 3. Running Local Quality Checks

Before opening a pull request, run the full local verification suite:

```bash
# 1. Format, vet, and lint Go code
make lint

# 2. Run unit and 28-scenario security matrix tests with the race detector
make test

# 3. Build manager, modelctl, and gke-e2e binaries
make build

# 4. Lint and render the Helm chart
make helm-lint

# 5. Check Terraform formatting and schema validation
make terraform-validate
```

Or run all checks in a single command:

```bash
make check
```

To run tests across all three integrated repositories (`llm-release-gate`, `ai-model-provenance-gate`, and `gke-model-admission-controller`):

```bash
make test-all-repos
```

---

## 4. Submitting Changes

1. Fork the repository and create a topic branch from `main`.
2. Add or update unit/security tests in [`tests/`](tests/) or package `*_test.go` files for any behavioral change.
3. Update relevant documentation under [`docs/`](docs/) and verify all CLI flags and YAML examples match the implementation.
4. Ensure no environment-specific GCP project IDs, service account emails, `.tfstate`, `tfplan`, or private keys (`.key`, `.pem`) are included in the diff.
5. Open a Pull Request using the [Pull Request Template](.github/PULL_REQUEST_TEMPLATE.md).
