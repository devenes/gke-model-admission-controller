# Tutorial 13: Testing Adversarial Security Failures (`N-01` – `N-28`)

> **Series Navigation:** [12 Deploying an Approved Model Workload](12-deploying-an-approved-model-workload.md) ← **13 (Final Tutorial)**

---

## 1. What you will learn

- How to test negative security scenarios against the live GKE `ValidatingAdmissionWebhook` using the manifests in [`examples/`](../../examples/).
- How to run the automated 42-scenario real-cluster verification suite (`./bin/gke-e2e`) covering all 10 positive scenarios (`P-01`–`P-10`), 28 negative scenarios (`N-01`–`N-28`), and 4 resilience scenarios (`R-01`–`R-04`).

---

## 2. Prerequisites

- Completed [Tutorials 01–12](12-deploying-an-approved-model-workload.md) with `ClusterModelTrustPolicy/default` in `Enforce` mode and `ModelArtifact/gemma-2b-it` in `Verified` phase.

---

## 3. Architecture context

A security control is only as trustworthy as its negative tests. **GKE Model Admission Controller** includes both standalone negative YAML manifests in [`examples/`](../../examples/) and an automated real-GKE verification harness ([`cmd/gke-e2e/main.go`](../../cmd/gke-e2e/main.go)) that pushes 21 distinct tampered/malicious OCI model packages to Artifact Registry and verifies that every single one is deterministically rejected.

---

## 4. Required input files

- [`examples/negative-unannotated-vllm.yaml`](../../examples/negative-unannotated-vllm.yaml) (`N-26`: unannotated model-serving pod in protected namespace)
- [`examples/negative-mutable-runtime-tag.yaml`](../../examples/negative-mutable-runtime-tag.yaml) (`N-23`: mutable `:latest` runtime container tag)
- [`examples/negative-unauthorized-exemption.yaml`](../../examples/negative-unauthorized-exemption.yaml) (`N-27`: self-assigned `trust.gke.dev/exempt: "true"`)
- [`cmd/gke-e2e/main.go`](../../cmd/gke-e2e/main.go) (`./bin/gke-e2e`)

---

## 5. Step-by-step instructions

1. Test **Scenario `N-26`** (`examples/negative-unannotated-vllm.yaml`): attempt to deploy a `vllm` container in `ai-serving` **without** a `trust.gke.dev/model-artifact` annotation.
2. Test **Scenario `N-23`** (`examples/negative-mutable-runtime-tag.yaml`): attempt to deploy a workload referencing Verified `gemma-2b-it`, but using a mutable `:latest` runtime image tag.
3. Test **Scenario `N-27`** (`examples/negative-unauthorized-exemption.yaml`): attempt to bypass admission by adding `trust.gke.dev/exempt: "true"` as an unauthorized ServiceAccount.
4. Run the complete 42-scenario `./bin/gke-e2e` suite against your GKE cluster.

---

## 6. Commands

```bash
# 1. Test N-26: Unannotated vLLM Deployment in protected namespace (Expected: DENIED)
kubectl apply -f examples/negative-unannotated-vllm.yaml

# 2. Test N-23: Mutable :latest runtime image tag (Expected: DENIED)
export PROJECT_ID="${PROJECT_ID:-your-gcp-project-id}"
sed "s/PROJECT_ID/${PROJECT_ID}/g" examples/negative-mutable-runtime-tag.yaml | kubectl apply -f -

# 3. Test N-27: Self-assigned exemption by unauthorized ServiceAccount (Expected: DENIED)
kubectl apply --as=system:serviceaccount:ai-serving:unauthorized-deployer \
  -f examples/negative-unauthorized-exemption.yaml

# 4. Run the full 42-scenario real-GKE verification suite
./bin/gke-e2e \
  --registry "${REGION:-us-west4}-docker.pkg.dev" \
  --repo-prefix "${PROJECT_ID}/models"
```

---

## 7. Expected output

### Output for Manual Negative Manifests (`N-26`, `N-23`, `N-27`)

```text
Error from server (Forbidden): error when creating "examples/negative-unannotated-vllm.yaml": admission webhook "validate-workloads.trust.gke.dev" denied the request: [MISSING_MODEL_ARTIFACT_REF] workload in protected namespace runs a model-serving container or carries model labels without trust.gke.dev/model-artifact annotation

Error from server (Forbidden): error when creating "STDIN": admission webhook "validate-workloads.trust.gke.dev" denied the request: [RUNTIME_IMAGE_NOT_DIGEST_PINNED] container "vllm" image "us-west4-docker.pkg.dev/PROJECT_ID/models/vllm-runtime:latest" must be pinned by @sha256: digest

Error from server (Forbidden): error when creating "examples/negative-unauthorized-exemption.yaml": admission webhook "validate-workloads.trust.gke.dev" denied the request: [UNAUTHORIZED_EXEMPTION] caller "system:serviceaccount:ai-serving:unauthorized-deployer" is not authorized to use trust.gke.dev/exempt in ClusterModelTrustPolicy
```

### Output for `./bin/gke-e2e` (42 Scenarios)

```text
  [PASS] P-01: Deployment with Verified ModelArtifact allowed
  [PASS] P-02: Direct Pod with Verified ModelArtifact allowed
  [PASS] P-03: StatefulSet with Verified ModelArtifact allowed
  [PASS] P-04: DaemonSet with Verified ModelArtifact allowed
  [PASS] P-05: Job with Verified ModelArtifact allowed
  [PASS] P-06: CronJob with Verified ModelArtifact allowed
  [PASS] P-07: Non-model utility Pod in protected namespace allowed
  [PASS] P-08: LoRA Deployment with Verified BaseModelRef allowed
  [PASS] P-09: Audit mode allows non-compliant workload with Kubernetes warning
  [PASS] P-10: Authorized exemption ServiceAccount allowed (and unauthorized blocked)
  [PASS] N-01 .. N-28: All 28 negative scenarios rejected with exact expected reason codes
  [PASS] R-01 .. R-04: All 4 resilience scenarios passed
=== ALL 42 LIVE GKE E2E SCENARIOS PASSED SUCCESSFULLY ===
```

---

## 8. How to verify success

Inspect the Prometheus denial metrics by reason code:

```bash
kubectl run metrics-check -n model-admission-system --rm -i --restart=Never \
  --image=curlimages/curl:8.10.1 -- \
  curl -s http://model-admission-controller-metrics.model-admission-system.svc.cluster.local:8080/metrics \
  | grep 'model_admission_decisions_total{decision="deny"'
```

---

## 9. Common errors

| Error | Cause | Fix |
|---|---|---|
| Negative workload was allowed with a `Warning:` | `ClusterModelTrustPolicy/default` was left in `Audit` mode | Patch `ClusterModelTrustPolicy/default` back to `spec.enforcementMode: Enforce` |

---

## 10. Security considerations

- **Fail-Closed Verification**: Every single negative scenario (`N-01` through `N-28`) verifies not just that an error occurred, but that the **exact expected cryptographic or policy failure reason code** was recorded in `ModelArtifact.status.failureReason` or returned by the `ValidatingAdmissionWebhook`.

---

## 11. What comes next

You have completed the 13-part tutorial series! Explore:
- [Operations Runbooks (`docs/operations/`)](../operations/day-two-operations.md) for key rotation, certificate rotation, and incident response.
- [Architecture Decision Records (`docs/adr/`)](../adr/ADR-001-verifier-api.md) for deep dives into the verification API, OCI v1.1 layout, and trust domain model.
