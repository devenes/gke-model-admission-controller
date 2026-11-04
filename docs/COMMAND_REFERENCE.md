# Command Reference (`COMMAND_REFERENCE.md`)

Every command and flag documented in this reference has been verified against the actual source code in:

- [`gke-model-admission-controller/Makefile`](../Makefile)
- [`gke-model-admission-controller/cmd/manager/main.go`](../cmd/manager/main.go)
- [`gke-model-admission-controller/cmd/modelctl/main.go`](../cmd/modelctl/main.go)
- [`gke-model-admission-controller/cmd/gke-e2e/main.go`](../cmd/gke-e2e/main.go)
- [`ai-model-provenance-gate/internal/cli/cli.go`](https://github.com/devenes/ai-model-provenance-gate)
- [`llm-release-gate/internal/cli/cli.go`](https://github.com/devenes/llm-release-gate)

---

## 1. `Makefile` Targets (`gke-model-admission-controller`)

| Command | Description |
|---|---|
| `make all` | Runs `make test` followed by `make build`. |
| `make build` | Compiles `bin/manager`, `bin/modelctl`, and `bin/gke-e2e` with `CGO_ENABLED=0 -trimpath -ldflags="-s -w"`. |
| `make test` | Runs `go test -race -v ./...` across all packages in `gke-model-admission-controller`. |
| `make test-all-repos` | Runs `go test -race ./...` across `../llm-release-gate`, `../ai-model-provenance-gate`, and `gke-model-admission-controller`. |
| `make lint` | Checks `gofmt -s -l .`, runs `go vet ./...`, and runs `golangci-lint run ./...` when installed. |
| `make check` | Runs `lint`, `test`, `build`, `helm-lint`, and `terraform-validate`. |
| `make helm-lint` | Runs `helm lint ./charts/model-admission-controller` and validates template rendering. |
| `make helm-template` | Renders the Helm chart manifests to stdout for namespace `model-admission-system`. |
| `make tf-fmt` | Runs `terraform -chdir=terraform fmt -check -recursive`. |
| `make terraform-validate` | Runs `make tf-fmt` followed by `terraform -chdir=terraform validate`. |
| `make test-e2e-gke` | Builds binaries and executes `./bin/gke-e2e` using `REGION`, `PROJECT_ID`, and `RUNTIME_IMAGE`. |

---

## 2. `manager` Binary (`./bin/manager`)

The Kubernetes controller manager and `ValidatingAdmissionWebhook` server deployed in GKE (`cmd/manager/main.go`).

```bash
./bin/manager [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--metrics-bind-address` | `:8080` | TCP address the Prometheus metrics endpoint binds to. |
| `--health-probe-bind-address` | `:8081` | TCP address the `/healthz` and `/readyz` probe endpoints bind to. |
| `--webhook-port` | `9443` | TCP port the `ValidatingAdmissionWebhook` server binds to (`/validate-workloads`). |
| `--webhook-cert-dir` | `/tmp/k8s-webhook-server/serving-certs` | Directory containing `tls.crt` and `tls.key` mounted from the `cert-manager` Secret. |
| `--leader-elect` | `true` | Enables leader election (`gke-model-admission-controller.trust.gke.dev`) for controllers. |
| `--registry-scheme` | `https` | HTTP scheme used for OCI registry requests (`https` or `http`). |

---

## 3. `modelctl` CLI (`./bin/modelctl`)

Packages and publishes OCI v1.1 Model Bundles and their 4 Referrer artifacts (or daemonless container images) to Google Artifact Registry (`cmd/modelctl/main.go`).

```bash
./bin/modelctl [push] [flags]
```

| Flag | Default | Description |
|---|---|---|
| `--registry` | `us-west4-docker.pkg.dev` | OCI v1.1 registry hostname. |
| `--repository` | `PROJECT_ID/models/gemma-2b-it` | OCI repository path (`<project>/<repo>/<model>`). |
| `--model-name` | `gemma-2b-it` | Name for generated `ModelArtifact` metadata. |
| `--namespace` | `ai-serving` | Target Kubernetes namespace for generated `ModelArtifact` YAML. |
| `--runtime-image` | `""` | Digest-pinned serving container image (`@sha256:<64-hex>`) bound into Release Qualification. |
| `--serving-config-digest` | `sha256:5555...5555` | `sha256:<64-hex>` digest of the serving configuration bound into Release Qualification. |
| `--scheme` | `https` | Registry HTTP scheme (`https` or `http`). |
| `--model-dir` | `""` | Path to local model bundle directory. If omitted, uses built-in synthetic fixture layers. |
| `--bundle-manifest` | `""` | Path to pre-generated `bundle-manifest.json`. |
| `--oms-signature` | `""` | Path to pre-generated OpenSSF Model Signing (`oms.sig.json`) file. |
| `--bom` | `""` | Path to pre-generated CycloneDX 1.7 AI/ML-BOM (`bom.cdx.json`) file. |
| `--eval-report` | `""` | Path to pre-generated `llm-release-gate` `report.json` file. |
| `--eval-attestation` | `""` | Path to pre-generated native DSSE evaluation attestation (`evaluation.dsse.json`). |
| `--release-qualification` | `""` | Path to pre-generated DSSE release qualification attestation (`attestation.json`). |
| `--out-policy` | `""` | Optional path to write generated `ClusterModelTrustPolicy` YAML. |
| `--out-model-artifact` | `""` | Optional path to write generated `ModelArtifact` YAML. |
| `--push-binary-image` | `""` | Path to a `linux/amd64` binary to package and push as a rootless (`65532:65532`) OCI container image. |
| `--tag` | `v0.1.0` | Image tag when `--push-binary-image` is used. |

---

## 4. `gke-e2e` Live GKE Verification Runner (`./bin/gke-e2e`)

Executes the complete live GKE + Google Artifact Registry verification suite: 10 positive scenarios (`P-01`..`P-10`), 28 negative security scenarios (`N-01`..`N-28`), and 4 operational resilience scenarios (`R-01`..`R-04`) (`cmd/gke-e2e/main.go`).

```bash
./bin/gke-e2e \
  --registry-host REGION-docker.pkg.dev \
  --repo-prefix PROJECT_ID/models \
  --runtime-image "REGION-docker.pkg.dev/PROJECT_ID/models/gke-model-admission-controller@sha256:<64-hex>" \
  --output-dir artifacts/final-execution \
  --examples-dir examples
```

| Flag | Default | Description |
|---|---|---|
| `--registry-host` | `us-west4-docker.pkg.dev` | Google Artifact Registry hostname. |
| `--repo-prefix` | `PROJECT_ID/models` | Repository prefix where E2E model bundles and referrers are published. |
| `--runtime-image` | *(required)* | Digest-pinned runtime image URI (`@sha256:<64-hex>`). |
| `--output-dir` | `artifacts/final-execution` | Output directory for `gke-e2e-results.json`, `admission-security-matrix.json`, and `registry-compatibility-report.md`. |
| `--examples-dir` | `examples` | Directory where sample Kubernetes manifests are written. |

---

## 5. `ai-model-provenance` CLI (`ai-model-provenance-gate`)

| Subcommand | Verified Usage & Flags |
|---|---|
| `keygen` | `ai-model-provenance keygen --output-dir <dir> [--name <prefix>]` |
| `inspect` | `ai-model-provenance inspect --bundle <dir> [--output <dir>] [--base-bundle <dir>] [--expected-base-bundle-id <sha256:hex>]` |
| `oms sign` | `ai-model-provenance oms sign --bundle <dir> --private-key <key.pem> --output <file> [--key-id <id>] [--sigstore-bundle=true]` |
| `oms verify` | `ai-model-provenance oms verify --bundle <dir> --signature <file> [--public-key <pub.pem>] [--key-id <id>] [--mode native\|external-cli]` |
| `bom generate` | `ai-model-provenance bom generate --bundle <dir> --output <file\|dir> [--spec-version 1.7\|1.6] [--base-bundle <dir>] [--expected-base-bundle-id <sha256:hex>] [--name <model-name>]` |
| `bom validate` | `ai-model-provenance bom validate --bom <file> [--bundle <dir>]` |
| `evidence bind-eval` | `ai-model-provenance evidence bind-eval --bundle <dir> --evaluation-report <report.json> --private-key <key.pem> --output <file> [--key-id <id>] [--producer-role operator-endorser\|trusted-evaluator]` |
| `evidence verify` | `ai-model-provenance evidence verify --bundle <dir> --evaluation-report <report.json> [--evaluation-attestation <att.json>] [--public-key <pub.pem>] [--key-id <id>]` |
| `gate evaluate` | `ai-model-provenance gate evaluate --bundle <dir> [--policy <policy.yaml>] [--oms-signature <sig>] [--evaluation-report <report.json>] [--evaluation-attestation <att.json>] [--signing-key <key.pem>] [--signing-key-id <id>] [--runtime-image <uri@sha256:...>] [--serving-config <file>] [--base-bundle <dir>] [--expected-base-bundle-id <sha256:hex>] [--output <dir>]` |
| `release verify` | `ai-model-provenance release verify --bundle <dir> --attestation <attestation.json> [--deployment <deployment.yaml>] [--policy <policy.yaml>] [--public-key <pub.pem>] [--base-bundle <dir>] [--output <dir>]` |
| `version` | `ai-model-provenance version [--json]` |

---

## 6. `llm-release-gate` CLI (`llm-release-gate`)

| Subcommand | Verified Usage & Flags |
|---|---|
| `validate` | `llm-release-gate validate --suite <suite.yaml> [--candidate <candidate.yaml>] [--baseline <report.json>]` |
| `run` | `llm-release-gate run --suite <suite.yaml> --candidate <candidate.yaml> --output <dir\|file.json> [--baseline <report.json>] [--bundle-manifest <bundle-manifest.json>] [--signing-key <evaluator.key>] [--signing-key-id <id>] [--attestation-output <evaluation.dsse.json>]` |
| `compare` | `llm-release-gate compare --baseline <baseline.json> --candidate <candidate.json> [--suite <suite.yaml>] [--output <dir\|file.json>]` |
| `version` | `llm-release-gate version [--json]` |
