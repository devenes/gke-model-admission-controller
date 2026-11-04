# Hands-On Workshop & Live Demo Runbook

**Cryptographic AI Model Supply-Chain Security on Google Kubernetes Engine (GKE)**

This step-by-step workshop guide is designed for **conference workshops, customer architecture sessions, security engineering bootcamps, and live technical demos**. Every command in this runbook uses real repository fixtures, real CLI flags, and deterministic outputs.

---

## 1. Workshop Formats & Pacing

You can run this workshop in two formats depending on audience setup:

| Format | Duration | Cloud Account Needed by Attendees? | Best For |
| :--- | :--- | :--- | :--- |
| **Track A — Laptop Hands-On Lab (Local + In-Memory GKE Webhook Matrix)** | **45–60 min** | **No** (runs 100% locally on macOS/Linux) | Conference rooms, large audiences (20–200 attendees), university/security workshops |
| **Track B — Full Live GKE Cloud Workshop (Artifact Registry + GKE Standard)** | **75–90 min** | **Instructor only** (or 1 GCP sandbox project per attendee) | Platform engineering bootcamps, customer PoCs, live stage demos |

### Agenda & Time Allocation

| Module | Time | Focus | Track |
| :--- | :--- | :--- | :--- |
| **[Pre-Flight](#2-instructor-pre-flight-checklist-t-30-minutes)** | `T-30 min` | Provision GKE cluster & warm container caches before audience arrives | Track B |
| **[Module 1](#module-1-the-ai-supply-chain-blind-spot-10-min)** | `10 min` | Why container image signing (`vllm@sha256:...`) leaves AI models exposed | Track A & B |
| **[Module 2](#module-2-workspace-setup--build-10-min)** | `10 min` | Clone the 3-repo architecture and compile all 5 binaries | Track A & B |
| **[Module 3](#module-3-act-i--model-identity-oms-signing--aiml-bom-15-min)** | `15 min` | `bundle-identity/v1`, OpenSSF Model Signing (`OMS v1.0`), CycloneDX 1.7 AI/ML-BOM, and live byte-tamper attack | Track A & B |
| **[Module 4](#module-4-act-ii--behavioral-evaluation--5-point-release-gate-15-min)** | `15 min` | `llm-release-gate` DSSE attestation, 5-point `release-qualification/v1`, and key-confusion attack | Track A & B |
| **[Module 5](#module-5-act-iii--oci-v11-referrers--zero-weight-reconciliation-20-min)** | `20 min` | Publishing OCI v1.1 Referrers with `modelctl` and reconciling `ModelArtifact` without downloading weights | Track A (Simulated) & Track B (Live GKE) |
| **[Module 6](#module-6-act-iv--live-admission-enforcement--red-team-lab-15-min)** | `15 min` | Deploying approved models vs. 4 live red-team bypass attacks (`vLLM`, mutable tags, unauthorized exemption) | Track A & B |
| **[Module 7](#module-7-audience-qa-cheat-sheet--teardown-5-min)** | `5 min` | Top audience Q&A cheat sheet, Prometheus telemetry inspection, and environment cleanup | Track A & B |

---

## 2. Instructor Pre-Flight Checklist (`T-30 Minutes`)

> [!IMPORTANT]
> **If you are presenting Live on GKE (Track B):** Run this section **30 minutes before the session starts** so the audience does not wait ~12 minutes for GKE cluster creation during your talk.

### 2.1 Verify Local CLI Versions

```bash
go version          # Required: go1.24+ (tested with go1.27.3)
helm version --short
terraform version
kubectl version --client
gcloud version | head -n 1
```

### 2.2 Set Instructor Environment Variables (Track B Only)

```bash
export PROJECT_ID="YOUR_GCP_PROJECT_ID"
export REGION="us-west4"
export ZONE="us-west4-a"
export CLUSTER_NAME="gke-model-admission-demo"
export REGISTRY_HOST="${REGION}-docker.pkg.dev"
export REPO_PREFIX="${PROJECT_ID}/models"
```

### 2.3 Pre-Provision the GKE Cluster & Controller (Track B Only)

```bash
# 1. Configure Terraform variables
cat <<EOF > terraform/environments/demo/terraform.tfvars
project_id   = "${PROJECT_ID}"
region       = "${REGION}"
zone         = "${ZONE}"
cluster_name = "${CLUSTER_NAME}"
EOF

# 2. Provision VPC, Cloud NAT, Artifact Registry, Workload Identity, and 2-node CPU GKE Standard cluster
terraform -chdir=terraform init
terraform -chdir=terraform apply -var-file=environments/demo/terraform.tfvars -auto-approve

# 3. Connect kubectl to the cluster
gcloud container clusters get-credentials "${CLUSTER_NAME}" --zone "${ZONE}" --project "${PROJECT_ID}"
gcloud auth configure-docker "${REGISTRY_HOST}" --quiet

# 4. Install cert-manager and CRDs
helm repo add jetstack https://charts.jetstack.io && helm repo update
helm upgrade --install cert-manager jetstack/cert-manager \
  --namespace cert-manager --create-namespace --set crds.enabled=true --wait
kubectl apply -f config/crd/

# 5. Build and push the controller binary image using modelctl
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/manager-linux-amd64 ./cmd/manager
go build -o bin/modelctl ./cmd/modelctl

PUSH_OUT=$(./bin/modelctl \
  --registry "${REGISTRY_HOST}" \
  --repository "${REPO_PREFIX}/gke-model-admission-controller" \
  --tag "v0.1.0" \
  --push-binary-image ./bin/manager-linux-amd64)
echo "${PUSH_OUT}"

export RUNTIME_IMAGE=$(echo "${PUSH_OUT}" | awk -F'"' '/pushed_image_uri/ {print $4}')
export IMAGE_DIGEST=$(echo "${RUNTIME_IMAGE}" | awk -F'@' '{print $2}')

# 6. Install the 2-replica HA Admission Controller via Helm
helm upgrade --install model-admission-controller ./charts/model-admission-controller \
  --namespace model-admission-system --create-namespace \
  --set image.repository="${REGISTRY_HOST}/${REPO_PREFIX}/gke-model-admission-controller" \
  --set image.digest="${IMAGE_DIGEST}" \
  --set serviceAccount.gcpServiceAccount="gke-model-admission-controller@${PROJECT_ID}.iam.gserviceaccount.com" \
  --wait
```

Verify both webhook replicas are running before going on stage:

```bash
kubectl -n model-admission-system get pods
```

---

## Module 1: The AI Supply-Chain Blind Spot (`10 min`)

### Presenter Talking Points

1. **Start with a question for the audience:**
   *"How many of you verify container signatures or run vulnerability scans on your inference images—like `vllm/vllm-openai` or `text-generation-inference`—before deploying to Kubernetes?"*
2. **Reveal the architectural gap:**
   - In traditional microservices, the container image **is** the application.
   - In AI inference on Kubernetes, **the container image is only the execution engine**. The actual intelligence—`.safetensors` weights, `tokenizer.json`, `chat_template.jinja`, `generation_config.json`, and LoRA adapters—is mounted or fetched dynamically.
   - A pod running a 100% signed, vulnerability-free `vllm` container can still be compromised if:
     - `tokenizer.json` or `chat_template.jinja` is modified to inject a hidden system prompt.
     - `.pkl` / `.pt` pickle files or `config.json` `auto_map` hooks execute arbitrary Python on load.
     - An engineer deploys a fine-tuned checkpoint that failed behavioral safety/regression evaluation.
     - Someone re-uses a model-signing key to forge an evaluation pass.
3. **Show the 3-Repository Architecture Diagram** (open [`README.md`](../README.md) or [`docs/assets/architecture/system-architecture.svg`](assets/architecture/system-architecture.svg) on screen):
   - **`llm-release-gate`**: Behavioral evaluation & native evaluation-time DSSE signing (`evaluation-evidence/v1`).
   - **`ai-model-provenance-gate`**: Content-addressed `bundle-identity/v1`, OpenSSF Model Signing (`OMS v1.0`), `CycloneDX 1.7 AI/ML-BOM`, and 5-point `release-qualification/v1`.
   - **`gke-model-admission-controller`**: Zero-weight-download OCI v1.1 reconciler and zero-network-I/O `ValidatingAdmissionWebhook`.

---

## Module 2: Workspace Setup & Build (`10 min`)

### Step 2.1 — Clone the Three Repositories

> **Presenter Note:** Have attendees run this block in a single terminal window. All subsequent steps use `$WORKSHOP_DIR` so every command works regardless of their home directory.

```bash
export WORKSHOP_DIR="/tmp/ai-supply-chain-workshop"
rm -rf "${WORKSHOP_DIR}"
mkdir -p "${WORKSHOP_DIR}/work" && cd "${WORKSHOP_DIR}"

git clone https://github.com/devenes/llm-release-gate.git
git clone https://github.com/devenes/ai-model-provenance-gate.git
git clone https://github.com/devenes/gke-model-admission-controller.git
```

### Step 2.2 — Build All Five Binaries

```bash
(cd "${WORKSHOP_DIR}/llm-release-gate" && make build)
(cd "${WORKSHOP_DIR}/ai-model-provenance-gate" && make build)
(cd "${WORKSHOP_DIR}/gke-model-admission-controller" && make build)
```

Verify the compiled binaries:

```bash
"${WORKSHOP_DIR}/llm-release-gate/bin/llm-release-gate" --version
"${WORKSHOP_DIR}/ai-model-provenance-gate/bin/ai-model-provenance" version
"${WORKSHOP_DIR}/gke-model-admission-controller/bin/modelctl" -h 2>&1 | head -n 5
```

### Step 2.3 — Inspect the Synthetic Model Bundle

> **Presenter Talking Point:** *"Why are we using a `<1 KB` synthetic `.safetensors` bundle in the workshop? Because SHA-256, Ed25519 DSSE signatures, and OCI v1.1 Referrer graphs are mathematically identical whether a `.safetensors` file is 200 bytes or 70 gigabytes—and our admission controller never downloads weight blobs anyway!"*

```bash
cd "${WORKSHOP_DIR}/gke-model-admission-controller"
ls -la examples/fixtures/minimal-safetensors/
```

Expected files on screen:
- `config.json` (model architecture metadata, zero `auto_map` hooks)
- `generation_config.json`
- `model.safetensors` (safe tensor format, no Python `pickle`)
- `tokenizer.json`

---

## Module 3: Act I — Model Identity, OMS Signing & AI/ML-BOM (`15 min`)

### Step 3.1 — Generate Separated Trust Domain Keys

> **Presenter Talking Point:** *"Notice we generate three distinct Ed25519 keypairs. In our trust model, the ML engineer who signs model weights (`ms-1`) cannot approve behavioral evaluation (`ev-1`) or mint a production release qualification (`ra-1`)."*

```bash
mkdir -p "${WORKSHOP_DIR}/work/keys" "${WORKSHOP_DIR}/work/out"

for role in model-signer evaluator release-authority; do
  "${WORKSHOP_DIR}/ai-model-provenance-gate/bin/ai-model-provenance" keygen \
    --output-dir "${WORKSHOP_DIR}/work/keys" \
    --name "${role}"
done

ls -la "${WORKSHOP_DIR}/work/keys"
```

### Step 3.2 — Compute Deterministic `bundle-identity/v1`

```bash
"${WORKSHOP_DIR}/ai-model-provenance-gate/bin/ai-model-provenance" inspect \
  --bundle ./examples/fixtures/minimal-safetensors \
  --output "${WORKSHOP_DIR}/work/out"

cat "${WORKSHOP_DIR}/work/out/bundle-manifest.json"
```

**Highlight to Audience:**
- Point out `schemaVersion: "bundle-identity/v1"`, the `bundleId` (`sha256:...`), and how every file (`config.json`, `model.safetensors`, `tokenizer.json`) is hashed and canonicalized with RFC 8785 JCS.

### Step 3.3 — Sign the Bundle with OpenSSF Model Signing (`OMS v1.0`) & Generate CycloneDX 1.7 AI/ML-BOM

```bash
# 1. Sign with OpenSSF Model Signing v1.0 (key-id: ms-1)
"${WORKSHOP_DIR}/ai-model-provenance-gate/bin/ai-model-provenance" oms sign \
  --bundle ./examples/fixtures/minimal-safetensors \
  --private-key "${WORKSHOP_DIR}/work/keys/model-signer.key" \
  --key-id ms-1 \
  --output "${WORKSHOP_DIR}/work/out/oms.sig.json"

# 2. Generate CycloneDX 1.7 AI/ML-BOM
"${WORKSHOP_DIR}/ai-model-provenance-gate/bin/ai-model-provenance" bom generate \
  --bundle ./examples/fixtures/minimal-safetensors \
  --output "${WORKSHOP_DIR}/work/out/bom.cdx.json"
```

### Step 3.4 — Live Audience Exercise: Flip One Byte in `tokenizer.json`

> **Presenter Prompt:** *"Let's simulate an insider or storage-bucket attack where someone subtly edits `tokenizer.json` after the model was signed."*

```bash
cp -R ./examples/fixtures/minimal-safetensors "${WORKSHOP_DIR}/work/tampered-bundle"
echo " " >> "${WORKSHOP_DIR}/work/tampered-bundle/tokenizer.json"

"${WORKSHOP_DIR}/ai-model-provenance-gate/bin/ai-model-provenance" oms verify \
  --bundle "${WORKSHOP_DIR}/work/tampered-bundle" \
  --signature "${WORKSHOP_DIR}/work/out/oms.sig.json" \
  --public-key "${WORKSHOP_DIR}/work/keys/model-signer.pub" \
  --key-id ms-1 || echo "EXPECTED FAILURE: Tampered tokenizer rejected!"
```

---

## Module 4: Act II — Behavioral Evaluation & 5-Point Release Gate (`15 min`)

### Step 4.1 — Run `llm-release-gate` with Evaluation-Time DSSE Binding

> **Presenter Talking Point:** *"Most teams treat evaluation reports as unsigned JSON files in CI artifacts. Here, `llm-release-gate` validates the `bundle-manifest.json` before running the suite and signs an in-toto Statement v1 DSSE envelope (`evaluation-evidence/v1`) whose subject is the exact `bundle-manifest.json` SHA-256 digest."*

```bash
"${WORKSHOP_DIR}/llm-release-gate/bin/llm-release-gate" run \
  --suite ./examples/fixtures/eval/suite.yaml \
  --candidate ./examples/fixtures/eval/candidate.yaml \
  --bundle-manifest "${WORKSHOP_DIR}/work/out/bundle-manifest.json" \
  --signing-key "${WORKSHOP_DIR}/work/keys/evaluator.key" \
  --signing-key-id ev-1 \
  --attestation-output "${WORKSHOP_DIR}/work/out/evaluation.dsse.json" \
  --output "${WORKSHOP_DIR}/work/out/eval"
```

### Step 4.2 — Live Red-Team Exercise: Attempt Key Confusion (Self-Approving Release with Model Signer Key)

> **Presenter Prompt:** *"What happens if the ML engineer tries to sign the Release Qualification attestation using their own `model-signer.key` (`ms-1`) instead of the Release Authority key (`ra-1`)?"*

```bash
export DEMO_RUNTIME_IMAGE="${RUNTIME_IMAGE:-us-west4-docker.pkg.dev/PROJECT_ID/models/gke-model-admission-controller@sha256:9ca52f2367a58106b6081085e6d3c9020409e03dbc34e21155f06600e8d83c93}"

"${WORKSHOP_DIR}/ai-model-provenance-gate/bin/ai-model-provenance" gate evaluate \
  --bundle ./examples/fixtures/minimal-safetensors \
  --oms-signature "${WORKSHOP_DIR}/work/out/oms.sig.json" \
  --evaluation-report "${WORKSHOP_DIR}/work/out/eval/report.json" \
  --evaluation-attestation "${WORKSHOP_DIR}/work/out/evaluation.dsse.json" \
  --policy ./examples/fixtures/policy.yaml \
  --signing-key "${WORKSHOP_DIR}/work/keys/model-signer.key" \
  --signing-key-id ms-1 \
  --runtime-image "${DEMO_RUNTIME_IMAGE}" \
  --serving-config ./examples/fixtures/serving-config.yaml \
  --output "${WORKSHOP_DIR}/work/out/release-bad" || echo "EXPECTED FAILURE: Key confusion blocked!"
```

### Step 4.3 — Mint the Valid 5-Point `release-qualification/v1` Attestation

Now sign with the legitimate `release-authority.key` (`ra-1`):

```bash
"${WORKSHOP_DIR}/ai-model-provenance-gate/bin/ai-model-provenance" gate evaluate \
  --bundle ./examples/fixtures/minimal-safetensors \
  --oms-signature "${WORKSHOP_DIR}/work/out/oms.sig.json" \
  --evaluation-report "${WORKSHOP_DIR}/work/out/eval/report.json" \
  --evaluation-attestation "${WORKSHOP_DIR}/work/out/evaluation.dsse.json" \
  --policy ./examples/fixtures/policy.yaml \
  --signing-key "${WORKSHOP_DIR}/work/keys/release-authority.key" \
  --signing-key-id ra-1 \
  --runtime-image "${DEMO_RUNTIME_IMAGE}" \
  --serving-config ./examples/fixtures/serving-config.yaml \
  --output "${WORKSHOP_DIR}/work/out/release"
```

---

## Module 5: Act III — OCI v1.1 Referrers & Zero-Weight Reconciliation (`20 min`)

### Track A (No Cloud Account): Run the In-Memory OCI v1.1 + Admission Pipeline

Attendees without a GCP project run the full OCI v1.1 Referrers + Reconciler + Admission Webhook verification pipeline locally with the race detector:

```bash
cd "${WORKSHOP_DIR}/gke-model-admission-controller"
go test -race -v ./tests -run "TestEndToEndPipeline|TestZeroWeightDownloadGuarantee|TestSixWorkloadKindsAdmission"
```

**Highlight to Audience:**
- `TestZeroWeightDownloadGuarantee` proves that the OCI registry server receives `0` `GET /v2/.../blobs/` requests for `.safetensors` weight layers (`weightBlobReads == 0`).

### Track B (Live GKE): Publish to Artifact Registry & Reconcile on GKE

```bash
cd "${WORKSHOP_DIR}/gke-model-admission-controller"

./bin/modelctl \
  --registry "${REGISTRY_HOST}" \
  --repository "${REPO_PREFIX}/gemma-2b-it" \
  --model-name "gemma-2b-it" \
  --namespace "ai-serving" \
  --runtime-image "${RUNTIME_IMAGE}" \
  --serving-config-digest "sha256:5555555555555555555555555555555555555555555555555555555555555555" \
  --out-policy "${WORKSHOP_DIR}/work/out/cluster-policy.yaml" \
  --out-model-artifact "${WORKSHOP_DIR}/work/out/model-artifact.yaml"

kubectl create namespace ai-serving --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f "${WORKSHOP_DIR}/work/out/cluster-policy.yaml"
kubectl apply -f "${WORKSHOP_DIR}/work/out/model-artifact.yaml"

# Watch the controller reconcile the OCI v1.1 Referrer graph in ~1 second
kubectl -n ai-serving get modelartifact gemma-2b-it -w
```

Inspect the verified status and 3-domain digest bridging:

```bash
kubectl -n ai-serving get modelartifact gemma-2b-it -o yaml
```

**Highlight to Audience:**
- `status.conditions`: `ArtifactResolved`, `SignatureVerified`, `BOMVerified`, `EvaluationVerified`, `ReleaseQualified`, and `Ready` are all `True`.
- `status.bundleId`, `status.bundleManifestDigest`, `status.policyDigest`, `status.allowedRuntimeImages`, and `status.verifiedAt` are populated in etcd—ready for `<5 ms` admission evaluation!

---

## Module 6: Act IV — Live Admission Enforcement & Red-Team Lab (`15 min`)

### Track A (Laptop Hands-On): Run the 28-Scenario Negative Security Matrix

```bash
cd "${WORKSHOP_DIR}/gke-model-admission-controller"
go test -race -v ./tests -run TestSecurityMatrix28Scenarios
```

Have attendees inspect how each of the 28 attack vectors (`N-01` through `N-28`) is deterministically denied with a structured reason code.

### Track B (Live GKE): Positive Deployment vs. Live Red-Team Attacks

#### 1. Deploy the Approved Model Workload (`ADMITTED`)

```bash
sed "s|us-west4-docker.pkg.dev/PROJECT_ID/models/gke-model-admission-controller@sha256:9ca52f2367a58106b6081085e6d3c9020409e03dbc34e21155f06600e8d83c93|${RUNTIME_IMAGE}|g" \
  examples/positive-deployment.yaml | kubectl apply -f -

kubectl -n ai-serving get deployment gemma-2b-serving
```

#### 2. Red-Team Attack 1: Shadow AI Deployment (`vllm` Without ModelArtifact Annotation)

> **Presenter Prompt:** *"What happens if a developer deploys a `vllm/vllm-openai` container with `--model google/gemma-2b-it` and omits the `trust.gke.dev/model-artifact` annotation to bypass verification?"*

```bash
kubectl apply -f examples/negative-unannotated-vllm.yaml
```

**Expected Live Result (`DENIED`):**
```text
Error from server (Forbidden): error when creating "examples/negative-unannotated-vllm.yaml": admission webhook "vworkload.trust.gke.dev" denied the request: [UNDECLARED_MODEL_WORKLOAD] workload appears to be an AI model-serving workload but is missing required annotation "trust.gke.dev/model-artifact"
```

#### 3. Red-Team Attack 2: Mutable Runtime Image Tag (`:latest`)

> **Presenter Prompt:** *"What if the model is verified, but the deployment uses a mutable `:latest` container tag instead of the digest-pinned runtime image from the Release Qualification attestation?"*

```bash
kubectl apply -f examples/negative-mutable-runtime-tag.yaml
```

**Expected Live Result (`DENIED`):**
```text
Error from server (Forbidden): error when creating "examples/negative-mutable-runtime-tag.yaml": admission webhook "vworkload.trust.gke.dev" denied the request: [MUTABLE_RUNTIME_IMAGE_REFERENCE] container "inference" image must be pinned by immutable @sha256:... digest
```

#### 4. Red-Team Attack 3: Unauthorized Self-Exemption Attempt

> **Presenter Prompt:** *"What if a developer discovers the break-glass annotation `trust.gke.dev/exempt: 'true'` in the documentation and puts it on their own Deployment?"*

```bash
kubectl apply -f examples/negative-unauthorized-exemption.yaml --as=developer@example.com
```

**Expected Live Result (`DENIED`):**
```text
Error from server (Forbidden): error when creating "examples/negative-unauthorized-exemption.yaml": admission webhook "vworkload.trust.gke.dev" denied the request: [UNAUTHORIZED_EXEMPTION] caller "developer@example.com" is not in ClusterModelTrustPolicy.spec.exemptions.allowedPrincipals
```

#### 5. Bonus Finale: Run the Automated 42-Scenario Live GKE Suite (`10 Positive + 28 Negative + 4 Resilience`)

```bash
./bin/gke-e2e \
  --registry-host "${REGISTRY_HOST}" \
  --repo-prefix "${REPO_PREFIX}" \
  --runtime-image "${RUNTIME_IMAGE}"
```

---

## Module 7: Audience Q&A Cheat Sheet & Teardown (`5 min`)

### Top 5 Audience Questions & Crisp Answers

1. **Q: *"Does the admission webhook call Artifact Registry on every Pod creation? What if Artifact Registry is slow?"***
   - **A:** **No.** The `ValidatingAdmissionWebhook` performs **zero external network I/O**. All OCI v1.1 discovery and DSSE/CycloneDX verification happen asynchronously in `ModelArtifactReconciler`, which caches the verified state in `ModelArtifact.status`. The webhook only reads in-cluster Kubernetes state (`<5 ms` p95 latency) and enforces a cryptographic freshness TTL (`maxVerificationAge`).
2. **Q: *"How do you verify a 70 GB model without downloading 70 GB into the controller pod?"***
   - **A:** Through **3-Domain Digest Bridging** ([`docs/ARCHITECTURE.md`](ARCHITECTURE.md)). The reconciler fetches only the OCI image manifest (~2 KB) and small metadata/attestation blobs (<50 KB), enforcing a hard `10 MiB` cap (`MaxReferrerBlobBytes`) and verifying that the OCI layer descriptors (`sha256:...`) match both `bundle-manifest.json` and the signed OpenSSF Model Signing (`OMS v1.0`) predicate.
3. **Q: *"What if the webhook pods crash? Can unverified models slip through?"***
   - **A:** **No.** `ValidatingWebhookConfiguration` sets `failurePolicy: Fail` and `sideEffects: None`, backed by 2 replicas, pod anti-affinity, and a `PodDisruptionBudget` (`minAvailable: 1`). If the webhook is unreachable, the Kubernetes API server fails closed.
4. **Q: *"How do we roll this out safely in an existing production cluster without breaking current workloads?"***
   - **A:** Start with `ClusterModelTrustPolicy.spec.enforcementMode: Audit` ([`docs/guides/audit-mode.md`](guides/audit-mode.md)). In `Audit` mode, violations are logged, recorded as Kubernetes Warning Events, and counted in Prometheus (`model_admission_requests_total`) while returning HTTP `warnings` to `kubectl apply` without blocking scheduling.
5. **Q: *"What about fine-tuned LoRA adapters loaded onto a base model?"***
   - **A:** Both the base model and every PEFT/LoRA adapter (`trust.gke.dev/lora-artifacts`) are independently verified as `ModelArtifact` resources, and the controller verifies cryptographic base-model digest binding (`LORA_BASE_MODEL_MISMATCH` if an adapter is paired with the wrong base model).

### Teardown Instructions

#### Local Laptop Cleanup (Track A & B)

```bash
rm -rf "${WORKSHOP_DIR}"
```

#### Cloud Infrastructure Teardown (Track B Only — After Workshop Ends)

```bash
terraform -chdir=terraform destroy -var-file=environments/demo/terraform.tfvars -auto-approve
```
