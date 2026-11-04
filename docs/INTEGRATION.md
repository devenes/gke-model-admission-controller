# Cross-Repository Integration Guide

## 1. Repository Topology & Responsibilities

`gke-model-admission-controller` integrates across three repositories to establish a tamper-evident AI model supply chain:

| Repository | Role | Public Contract |
|---|---|---|
| [`llm-release-gate`](https://github.com/devenes/llm-release-gate) | Behavioral & safety evaluation runner | Emits `llm-release-gate.report/v1` (`report.json`) and native DSSE evaluation attestations (`evaluation-evidence/v1`, `binding_mode: "NATIVE_EVALUATOR"`) via `--bundle-manifest`, `--signing-key`, `--signing-key-id`, `--attestation-output`. |
| [`ai-model-provenance-gate`](https://github.com/devenes/ai-model-provenance-gate) | Model bundle identity, OMS v1.0 signing, CycloneDX 1.7 AI/ML-BOM, and release qualification | Exposes pure, byte-oriented, zero-subprocess verification API `pkg/verify` ([ADR-001](adr/ADR-001-verifier-api.md)). |
| [`gke-model-admission-controller`](https://github.com/devenes/gke-model-admission-controller) | OCI v1.1 Referrers packager (`modelctl`), GKE controller reconciler, and `ValidatingAdmissionWebhook` (`manager`) | Reconciles `ClusterModelTrustPolicy` and `ModelArtifact` (`trust.gke.dev/v1alpha1`) and enforces workload admission across `Pod`, `Deployment`, `StatefulSet`, `DaemonSet`, `Job`, and `CronJob`. |

---

## 2. End-to-End Qualification & OCI Publishing Workflow

### Step 1: Generate Canonical Bundle Identity, OMS Signature, and CycloneDX 1.7 BOM

```bash
ai-model-provenance inspect \
  --bundle ./examples/fixtures/minimal-safetensors \
  --output ./out

ai-model-provenance oms sign \
  --bundle ./examples/fixtures/minimal-safetensors \
  --private-key ./keys/model-signer.key \
  --key-id ms-1 \
  --output ./out/oms.sig.json

ai-model-provenance bom generate \
  --bundle ./examples/fixtures/minimal-safetensors \
  --output ./out/bom.cdx.json
```

### Step 2: Execute Native `llm-release-gate` Evaluation with Cryptographic Binding

```bash
llm-release-gate run \
  --suite ./examples/fixtures/eval/suite.yaml \
  --candidate ./examples/fixtures/eval/candidate.yaml \
  --bundle-manifest ./out/bundle-manifest.json \
  --signing-key ./keys/evaluator.key \
  --signing-key-id ev-1 \
  --attestation-output ./out/evaluation.dsse.json \
  --output ./out/eval
```

### Step 3: Issue Release Qualification Attestation (`release-qualification/v1`)

```bash
ai-model-provenance gate evaluate \
  --bundle ./examples/fixtures/minimal-safetensors \
  --oms-signature ./out/oms.sig.json \
  --evaluation-report ./out/eval/report.json \
  --evaluation-attestation ./out/evaluation.dsse.json \
  --policy ./examples/fixtures/policy.yaml \
  --signing-key ./keys/release-authority.key \
  --signing-key-id ra-1 \
  --runtime-image REGION-docker.pkg.dev/PROJECT_ID/models/gke-model-admission-controller@sha256:9ca52f2367a58106b6081085e6d3c9020409e03dbc34e21155f06600e8d83c93 \
  --serving-config ./examples/fixtures/serving-config.yaml \
  --output ./out/release
```

### Step 4: Publish OCI v1.1 Model Bundle & 4 Referrers via `modelctl`

```bash
./bin/modelctl push \
  --registry REGION-docker.pkg.dev \
  --repository PROJECT_ID/models/gemma-2b-it \
  --model-dir ./examples/fixtures/minimal-safetensors \
  --bundle-manifest ./out/bundle-manifest.json \
  --oms-signature ./out/oms.sig.json \
  --bom ./out/bom.cdx.json \
  --eval-attestation ./out/evaluation.dsse.json \
  --eval-report ./out/eval/report.json \
  --release-qualification ./out/release/attestation.json
```
