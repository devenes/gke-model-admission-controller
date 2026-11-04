# Testing & Verification Strategy

## 1. Multi-Tier Verification Architecture

`gke-model-admission-controller` is verified at three complementary levels:

1. **Cross-Repository Unit & Race Tests (`go test -race ./...`)**:
   - `llm-release-gate`: Native DSSE `evaluation-evidence/v1` generation and verification.
   - `ai-model-provenance-gate`: Public `pkg/verify` API covering `bundle-identity/v1`, OMS v1.0, CycloneDX 1.7 AI/ML-BOM, `evaluation-evidence/v1`, and `release-qualification/v1`.
   - `gke-model-admission-controller`: Policy resolution, OCI v1.1 Referrers discovery, zero-weight-download verification (`TestEndToEndZeroWeightDownloadAndAllSixWorkloadKinds`), and the complete 28-case negative security matrix (`TestFull28NegativeSecurityMatrix`).
2. **Real Google Artifact Registry OCI v1.1 + Referrers Verification**:
   - Publishes real OCI v1.1 Model Bundles and 4 Referrer artifacts to `REGION-docker.pkg.dev/PROJECT_ID/models`.
   - Verifies `/v2/<repo>/referrers/<digest>`, `?artifactType=` filtering, and zero weight-blob downloads.
3. **Live GKE Standard Cluster End-to-End & Resilience Suite (`cmd/gke-e2e`)**:
   - Executes all **10 positive scenarios** (`P-01` .. `P-10`), all **28 negative security scenarios** (`N-01` .. `N-28`), and all **4 operational resilience scenarios** (`R-01` .. `R-04`) against the real GKE cluster (`CLUSTER_NAME`).

---

## 2. Running the Tests

### Local Unit & Integration Suite (All 3 Repositories)

```bash
make test-all-repos
make lint
make helm-lint
make terraform-validate
```

### Live GKE + Google Artifact Registry E2E & 28-Case Security Matrix

```bash
./bin/gke-e2e \
  --registry-host REGION-docker.pkg.dev \
  --repo-prefix PROJECT_ID/models \
  --runtime-image "REGION-docker.pkg.dev/PROJECT_ID/models/gke-model-admission-controller@sha256:9ca52f2367a58106b6081085e6d3c9020409e03dbc34e21155f06600e8d83c93" \
  --output-dir artifacts/final-execution \
  --examples-dir examples
```
