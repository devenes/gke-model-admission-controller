# Quick Start Guide

This guide walks you through the shortest reproducible path to build, test, and validate **GKE Model Admission Controller** locally, followed by deploying and enforcing model admission on Google Kubernetes Engine (GKE).

---

## 1. Prerequisites

Ensure the following tools are installed:

- **Go** `1.24+` (tested with `go1.27.3`)
- **Helm** `v3.14+`
- **Terraform** `v1.6+`
- **kubectl** and **gcloud** CLI (required only for live GKE deployment)

Clone the three integrated repositories into a shared parent directory:

```bash
mkdir -p ai-supply-chain && cd ai-supply-chain
git clone https://github.com/devenes/llm-release-gate.git
git clone https://github.com/devenes/ai-model-provenance-gate.git
git clone https://github.com/devenes/gke-model-admission-controller.git
cd gke-model-admission-controller
```

---

## 2. Run Local Verification & Security Matrix (Under 60 Seconds, No Cloud Account Required)

You can build all binaries, validate the Helm chart and Terraform configuration, and run the complete unit + 28-scenario negative security matrix locally without a GCP account:

```bash
# Build manager, modelctl, and gke-e2e binaries
make build

# Run gofmt, go vet, unit tests, 6-workload-kind admission tests,
# zero-weight-download tests, and the 28-scenario negative security matrix
make test

# Run Helm chart lint + template rendering and Terraform validation
make helm-lint
make terraform-validate
```

Expected output from `make test`:

```text
ok  	github.com/devenes/gke-model-admission-controller/internal/policy
ok  	github.com/devenes/gke-model-admission-controller/internal/registry
ok  	github.com/devenes/gke-model-admission-controller/tests
```

---

## 3. Run the End-to-End Local CLI Qualification Flow (Synthetic Fixtures)

Using the included `<1 KB` synthetic model bundle in [`examples/fixtures/minimal-safetensors/`](../examples/fixtures/minimal-safetensors/), you can run the complete three-domain cryptographic signing, evaluation, and release qualification flow locally:

```bash
# Build the companion CLIs
(cd ../llm-release-gate && make build)
(cd ../ai-model-provenance-gate && make build)

mkdir -p /tmp/gke-model-tutorial/keys /tmp/gke-model-tutorial/out

# 1. Generate isolated Ed25519 keys for the 3 trust domains
../ai-model-provenance-gate/bin/ai-model-provenance keygen --output-dir /tmp/gke-model-tutorial/keys --name model-signer
../ai-model-provenance-gate/bin/ai-model-provenance keygen --output-dir /tmp/gke-model-tutorial/keys --name evaluator
../ai-model-provenance-gate/bin/ai-model-provenance keygen --output-dir /tmp/gke-model-tutorial/keys --name release-authority

# 2. Compute deterministic bundle-identity/v1
../ai-model-provenance-gate/bin/ai-model-provenance inspect \
  --bundle ./examples/fixtures/minimal-safetensors \
  --output /tmp/gke-model-tutorial/out

# 3. Sign model bundle with OpenSSF Model Signing (OMS v1.0)
../ai-model-provenance-gate/bin/ai-model-provenance oms sign \
  --bundle ./examples/fixtures/minimal-safetensors \
  --private-key /tmp/gke-model-tutorial/keys/model-signer.key \
  --key-id ms-1 \
  --output /tmp/gke-model-tutorial/out/oms.sig.json

# 4. Generate CycloneDX 1.7 AI/ML-BOM
../ai-model-provenance-gate/bin/ai-model-provenance bom generate \
  --bundle ./examples/fixtures/minimal-safetensors \
  --output /tmp/gke-model-tutorial/out/bom.cdx.json

# 5. Run behavioral evaluation with native evaluation-time DSSE attestation binding
../llm-release-gate/bin/llm-release-gate run \
  --suite ./examples/fixtures/eval/suite.yaml \
  --candidate ./examples/fixtures/eval/candidate.yaml \
  --bundle-manifest /tmp/gke-model-tutorial/out/bundle-manifest.json \
  --signing-key /tmp/gke-model-tutorial/keys/evaluator.key \
  --signing-key-id ev-1 \
  --attestation-output /tmp/gke-model-tutorial/out/evaluation.dsse.json \
  --output /tmp/gke-model-tutorial/out/eval

# 6. Issue signed Release Qualification Attestation (5-point digest binding)
../ai-model-provenance-gate/bin/ai-model-provenance gate evaluate \
  --bundle ./examples/fixtures/minimal-safetensors \
  --oms-signature /tmp/gke-model-tutorial/out/oms.sig.json \
  --evaluation-report /tmp/gke-model-tutorial/out/eval/report.json \
  --evaluation-attestation /tmp/gke-model-tutorial/out/evaluation.dsse.json \
  --policy ./examples/fixtures/policy.yaml \
  --signing-key /tmp/gke-model-tutorial/keys/release-authority.key \
  --signing-key-id ra-1 \
  --runtime-image "us-west4-docker.pkg.dev/PROJECT_ID/models/gke-model-admission-controller@sha256:9ca52f2367a58106b6081085e6d3c9020409e03dbc34e21155f06600e8d83c93" \
  --serving-config ./examples/fixtures/serving-config.yaml \
  --output /tmp/gke-model-tutorial/out/release
```

---

## 4. Deploy to Google Kubernetes Engine (GKE)

To provision the complete Google Cloud infrastructure via Terraform and deploy the controller to a real GKE Standard cluster:

```bash
# 1. Configure your GCP variables
cp terraform/environments/demo/terraform.tfvars.example terraform/environments/demo/terraform.tfvars
# Edit terraform/environments/demo/terraform.tfvars to set project_id = "YOUR_GCP_PROJECT_ID"

# 2. Provision VPC, Cloud NAT, Firewall, Artifact Registry, IAM, and GKE Standard cluster
terraform -chdir=terraform init
terraform -chdir=terraform plan -var-file=environments/demo/terraform.tfvars
terraform -chdir=terraform apply -var-file=environments/demo/terraform.tfvars

# 3. Configure kubectl credentials
gcloud container clusters get-credentials CLUSTER_NAME --zone ZONE --project PROJECT_ID

# 4. Install cert-manager, CRDs, and the GKE Model Admission Controller Helm chart
helm repo add jetstack https://charts.jetstack.io && helm repo update
helm upgrade --install cert-manager jetstack/cert-manager \
  --namespace cert-manager --create-namespace --set crds.enabled=true --wait

kubectl apply -f config/crd/

helm upgrade --install model-admission-controller ./charts/model-admission-controller \
  --namespace model-admission-system --create-namespace \
  --set image.repository="REGION-docker.pkg.dev/PROJECT_ID/models/gke-model-admission-controller" \
  --set image.digest="sha256:9ca52f2367a58106b6081085e6d3c9020409e03dbc34e21155f06600e8d83c93" \
  --set serviceAccount.gcpServiceAccount="gke-model-admission-controller@PROJECT_ID.iam.gserviceaccount.com" \
  --wait
```

---

## Next Steps

- Follow the **[13-Part Tutorial Series](tutorials/01-understanding-ai-model-admission.md)** for deep-dive walkthroughs of every stage.
- Read **[Architecture & 3-Domain Digest Bridging](ARCHITECTURE.md)** to understand the internal verification pipeline.
- Review **[Command Reference](COMMAND_REFERENCE.md)** for all CLI flags and Makefile targets.
