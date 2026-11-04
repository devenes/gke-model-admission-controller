# Local Development Guide

You can develop, test, and validate all cryptographic verification, OCI Referrers resolution, policy evaluation, and Kubernetes admission webhook logic locally **without** a GKE cluster or Google Cloud credentials.

---

## 1. Repository Layout

```text
gke-model-admission-controller/
├── api/v1alpha1/                  # ModelArtifact & ClusterModelTrustPolicy CRD Go types
├── charts/model-admission-controller/ # Helm chart (CRDs, RBAC, Deployment, Webhook, Cert, PDB)
├── cmd/
│   ├── controller/                # Main Kubernetes operator & admission webhook binary
│   ├── modelctl/                  # OCI v1.1 model bundle + 4-referrer packager CLI
│   └── gke-e2e/                   # 42-scenario real-GKE verification & security runner
├── config/crd/bases/              # Standalone CustomResourceDefinition YAML manifests
├── docs/                          # Architecture, concepts, guides, tutorials, operations, ADRs
├── examples/                      # Synthetic fixtures, policies, and positive/negative workloads
├── internal/
│   ├── controller/                # ModelArtifact & ClusterModelTrustPolicy reconcilers
│   ├── metrics/                   # 11 Prometheus metrics registered with controller-runtime
│   ├── policy/                    # 9-rule trust policy evaluator & RFC 8785 JCS policyDigest
│   ├── registry/                  # Zero-weight OCI v1.1 client & Referrers resolver
│   └── webhook/                   # ValidatingAdmissionWebhook (Pod/Deployment/StatefulSet/DaemonSet/Job/CronJob)
├── terraform/                     # Google Cloud VPC, GKE, Artifact Registry, IAM, Monitoring
└── tests/                         # End-to-end pipeline & 28-scenario security matrix tests
```

---

## 2. Building All Binaries

From `/path/to/gke-model-admission-controller`:

```bash
make build
```

This compiles:
- `./bin/controller`
- `./bin/modelctl`
- `./bin/gke-e2e`

To also build the upstream CLI tools (`ai-model-provenance` and `llm-release-gate`) into `./bin/`:

```bash
go build -C ../ai-model-provenance-gate -o ../gke-model-admission-controller/bin/ai-model-provenance ./cmd/ai-model-provenance
go build -C ../llm-release-gate -o ../gke-model-admission-controller/bin/llm-release-gate ./cmd/llm-release-gate
```

---

## 3. Running Unit & Race-Detector Tests

Run the full test suite with the Go race detector enabled across all three repositories:

```bash
# 1. llm-release-gate
(cd ../llm-release-gate && go test -race ./...)

# 2. ai-model-provenance-gate
(cd ../ai-model-provenance-gate && go test -race ./...)

# 3. gke-model-admission-controller
go test -race ./...
```

### Key Local Test Suites in `gke-model-admission-controller`

| Package | File | What It Verifies Locally |
|---|---|---|
| `internal/registry` | [`registry_test.go`](../../internal/registry/registry_test.go) | Spins up an in-memory `httptest.NewServer(registry.New())` OCI v1.1 registry, pushes synthetic model + 4 referrers, and asserts `/v2/.../blobs/sha256:<weight>` hits equal **zero** |
| `internal/policy` | [`evaluator_test.go`](../../internal/policy/evaluator_test.go) | Tests all 9 policy evaluation rules, `policyDigest` JCS canonicalization, runtime image digest pinning, and serving config digest matching |
| `tests` | [`pipeline_test.go`](../../tests/pipeline_test.go) | End-to-end in-memory OCI push + `pkg/verify` 7-step verification + policy evaluation |
| `tests` | [`security_matrix_test.go`](../../tests/security_matrix_test.go) | All 28 adversarial negative security scenarios (`N-01` through `N-28`) against in-memory OCI artifacts and the admission webhook handler |

---

## 4. Running Static Analysis & Chart Validation

```bash
go vet ./...
golangci-lint run ./...
helm lint ./charts/model-admission-controller
terraform -chdir=terraform fmt -check -recursive
terraform -chdir=terraform validate
```

---

## 5. Testing Synthetic Fixtures Locally

You can run the complete 3-tool CLI workflow locally against [`examples/fixtures/minimal-safetensors`](../../examples/fixtures/minimal-safetensors) using an ephemeral local directory—see [Quickstart Guide](../QUICKSTART.md) for the copy-pasteable commands.
