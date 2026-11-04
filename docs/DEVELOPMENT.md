# Developer Guide (`DEVELOPMENT.md`)

This guide covers setting up a local development environment for `gke-model-admission-controller`, navigating the Go package structure, running tests, and validating changes against a real GKE cluster.

---

## 1. Multi-Repository Workspace Layout

`gke-model-admission-controller` integrates directly with [`ai-model-provenance-gate`](https://github.com/devenes/ai-model-provenance-gate) (`pkg/verify`) and [`llm-release-gate`](https://github.com/devenes/llm-release-gate). For local cross-repository development, clone all three repositories into the same parent directory:

```text
workspace/
├── llm-release-gate/              # Behavioral evaluation & native evaluation-evidence/v1 signing
├── ai-model-provenance-gate/      # bundle-identity/v1, OMS v1.0, CycloneDX 1.7, release-qualification/v1, pkg/verify
└── gke-model-admission-controller/ # OCI v1.1 Referrers client, CRDs, Reconcilers, ValidatingAdmissionWebhook
```

[`go.mod`](../go.mod) includes local `replace` directives pointing to `../ai-model-provenance-gate` and `../llm-release-gate` so changes to `pkg/verify` take effect immediately during local compilation and testing.

---

## 2. Package Architecture

| Package | Path | Responsibility |
|---|---|---|
| `v1alpha1` | [`api/v1alpha1/`](../api/v1alpha1/) | Go types, condition constants, structured failure reason codes, and DeepCopy implementations for `ClusterModelTrustPolicy` and `ModelArtifact`. |
| `policy` | [`internal/policy/`](../internal/policy/) | Resolves trusted public keys from inline PEM or Kubernetes `Secret` references, computes SPKI `sha256:<64-hex>` fingerprints, computes the RFC 8785 JCS `policyDigest`, and evaluates non-self-assignable admission exemptions (`IsExempt`). |
| `registry` | [`internal/registry/`](../internal/registry/) | OCI Distribution v1.1 HTTP client (`Client`), Google Cloud Workload Identity token provider (`GCPMetadataTokenProvider`), `/v2/<repo>/referrers/<digest>` discovery, `MaxMetadataBlobBytes` (`16 MiB`) enforcement, OCI bundle/image packager (`PublishModelBundleAndEvidence`, `PublishBinaryContainerImage`), and in-memory OCI v1.1 mock registry (`MockOCIRegistry`) for unit tests. |
| `verification` | [`internal/verification/`](../internal/verification/) | 7-stage supply-chain verification engine (`Engine.Verify`) that orchestrates registry descriptor reads and calls `ai-model-provenance-gate/pkg/verify` in memory. |
| `controller` | [`internal/controller/`](../internal/controller/) | `ClusterModelTrustPolicyReconciler`, `ModelArtifactReconciler` (with `GenerationChangedPredicate` and `Secret` watch triggers), and thread-safe `PolicyStore`. |
| `admission` | [`internal/admission/`](../internal/admission/) | `WorkloadValidator` `ValidatingAdmissionWebhook` handler supporting `Pod`, `Deployment`, `StatefulSet`, `DaemonSet`, `Job`, and `CronJob` with zero network I/O. |
| `telemetry` | [`internal/telemetry/`](../internal/telemetry/) | Registers and exports the 11 Prometheus metrics on the `controller-runtime` metrics registry (`:8080/metrics`). |
| `cmd/manager` | [`cmd/manager/`](../cmd/manager/) | Main controller-manager entrypoint deployed inside the GKE cluster. |
| `cmd/modelctl` | [`cmd/modelctl/`](../cmd/modelctl/) | Helper CLI for packaging and pushing OCI v1.1 Model Bundles, 4 Referrer artifacts, and daemonless linux/amd64 container images to Google Artifact Registry. |
| `cmd/gke-e2e` | [`cmd/gke-e2e/`](../cmd/gke-e2e/) | Live GKE + Google Artifact Registry end-to-end test runner executing 10 positive, 28 negative security, and 4 operational resilience scenarios. |

---

## 3. Why Real GKE Is Used Instead of Kind or envtest

A deliberate engineering decision of this project is that **Kind, Minikube, and envtest are not used**:

1. **Local Fast Feedback (`go test -race ./...`)**:
   - Uses controller-runtime's `fake.Client` paired with [`registry.NewMockOCIRegistry()`](../internal/registry/mock_registry.go) (a full in-memory OCI v1.1 + Referrers HTTP server with byte-counter instrumentation).
   - Verifies all 7 reconciliation stages, all 6 workload kinds, zero weight-blob downloads (`FetchedWeightBytes() == 0`), and all 28 negative security scenarios in `<6 seconds`.
2. **Real GKE Standard Cluster for Kubernetes & Cloud Integration (`./bin/gke-e2e`)**:
   - Local Kubernetes emulators cannot faithfully reproduce Google Cloud Workload Identity Federation (`GKE_METADATA` server), Google Artifact Registry's real `/v2/<repo>/referrers/<digest>` implementation, GKE control-plane-to-private-node firewall rules (`172.16.0.0/28 -> :9443`), or Google Cloud Managed Service for Prometheus (`PodMonitoring`).
   - All Kubernetes API, CRD CEL immutability, `cert-manager` TLS, HA replica failover, and E2E tests are verified against a real CPU-only GKE Standard cluster provisioned by Terraform.

---

## 4. Building & Testing Locally

```bash
# Format, vet, and run golangci-lint (if installed)
make lint

# Run all Go unit and security matrix tests with the race detector
make test

# Run tests across all 3 sibling repositories
make test-all-repos

# Compile bin/manager, bin/modelctl, and bin/gke-e2e
make build

# Validate Helm chart and Terraform configuration
make helm-lint
make terraform-validate
```

---

## 5. Building and Publishing a Controller Image Without Docker Daemon

`modelctl` includes a built-in, daemonless OCI container image packager (`--push-binary-image`) that compiles or packages a static `linux/amd64` Go binary into a rootless (`65532:65532`) OCI v1.1 container image and pushes it directly to Google Artifact Registry:

```bash
mkdir -p bin
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/manager-linux-amd64 ./cmd/manager

./bin/modelctl \
  --registry REGION-docker.pkg.dev \
  --repository PROJECT_ID/models/gke-model-admission-controller \
  --push-binary-image bin/manager-linux-amd64 \
  --tag v0.1.0
```
