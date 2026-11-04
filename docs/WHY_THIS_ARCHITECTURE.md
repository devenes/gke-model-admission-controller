# Why This Architecture: Design Decisions & Trade-Offs

This document explains **why** `gke-model-admission-controller` is architected around asynchronous custom resource reconciliation (`ModelArtifact` + `ClusterModelTrustPolicy`), descriptor-only OCI v1.1 verification, three isolated cryptographic trust domains, and a zero-network-I/O `ValidatingAdmissionWebhook`.

---

## 1. Why Container Image Admission Alone Cannot Secure AI Models

Traditional Kubernetes admission controllers (such as Binary Authorization, Cosign/Policy Controller, or Kyverno image verification rules) verify the **serving container image** (for example, a `vllm` or `tritonserver` image).

In modern AI/LLM deployments on GKE, however, the serving container image is only the runtime engine. The actual model behavior and supply-chain risk live in decoupled artifacts:

1. **Model Weights & Tokenizers Are External to the Container Image**: Multi-gigabyte `*.safetensors` shards, `config.json`, `tokenizer.json`, and chat templates are pulled separately from OCI registries or object storage. A signed `vllm` container image can load tampered or unverified model weights unless the model bundle itself is verified.
2. **Cryptographic Signature != Behavioral Qualification**: A valid cryptographic signature from a model builder proves *who packaged the weights*, not whether the model passed safety, policy, or regression evaluation gates.
3. **Unbound Evaluation Reports Are Replayable**: If a JSON evaluation report (`report.json`) is not cryptographically bound to the exact model `bundle_id` at evaluation time by a trusted evaluator key, an operator can accidentally or maliciously reuse a passing evaluation from Model A to approve Model B.
4. **LoRA Adapters Introduce Composite Trust Dependencies**: A fine-tuned PEFT/LoRA adapter is only valid when loaded atop the exact base model `bundle_id` against which it was trained and evaluated.
5. **Runtime Configuration Drift**: Changing a model's serving configuration (system prompt policy, quantization flags, or chat template) or swapping the runtime image can invalidate evaluation guarantees even when the model weights are unchanged.

---

## 2. Why Verification Belongs in Reconciliation, Not the Admission Webhook

A naive model admission controller might query the OCI registry, discover referrers, download attestations, and verify cryptographic signatures **inside the `ValidatingAdmissionWebhook` HTTP handler** on every `Pod` creation.

That design fails in production Kubernetes clusters for four reasons:

| Problem with In-Webhook Registry Verification | How `gke-model-admission-controller` Solves It |
|---|---|
| **API Server Timeout & Tail Latency**: Kubernetes admission webhooks must respond within `timeoutSeconds` (`2s` in our chart). Registry token exchange + manifest fetch + 4 referrer fetches + blob downloads can spike latency during HPA scale-out bursts. | **Asynchronous Reconciliation (`ModelArtifactReconciler`)**: All OCI v1.1 registry and Referrers API calls happen asynchronously in the controller loop. The webhook performs **zero network I/O** and evaluates cached state in `<50ms`. |
| **Registry Outage Cascading into Pod Eviction Failure**: If the registry experiences a transient 5xx error while a node is cordoned and `Deployment` pods are rescheduled, in-webhook registry calls would block Pod recreation. | **Verified State Caching with TTL (`verificationTTLSeconds`)**: Once a `ModelArtifact` is verified (`status.verified == true`), new Pods referencing that model are admitted from cached Kubernetes state until `status.validUntil` expires or `policyDigest` changes. |
| **Thundering Herd on Scale-Out**: Scaling a `Deployment` from 1 to 50 replicas creates 50 `Pod` admission requests simultaneously, which would trigger 350+ redundant registry HTTP requests. | **Single Verification per `ModelArtifact` Generation**: The controller verifies the OCI artifact once per `ModelArtifact` spec/policy change and serves all 50 `Pod` admissions from the informer cache (`model_verification_cache_hits_total`). |
| **No Visibility Before Deployment**: Teams only discover a broken signature when `kubectl apply -f deployment.yaml` fails. | **Declarative `ModelArtifact.status`**: Platform and ML teams can inspect `kubectl get modelartifacts` and 7 granular status conditions (`ArtifactResolved`, `BundleIdentityVerified`, `OMSSignatureVerified`, `BOMVerified`, `EvaluationVerified`, `ReleaseQualificationPassed`, `Ready`) before deploying workloads. |

---

## 3. Why Zero Model Weight Downloads (`MaxMetadataBlobBytes = 16 MiB`)

LLM weight shards (`*.safetensors`) routinely range from `4 GiB` to `400+ GiB`. Downloading model weights inside a Kubernetes security controller pod (`256Mi` memory limit) would cause immediate `OOMKilled` crashes and saturate node network bandwidth.

Instead, the controller uses **Descriptor-Based Verification** ([ADR-002](adr/ADR-002-oci-model-artifact.md)):

1. Each file in the model bundle is published as a distinct layer in the OCI v1.1 Subject Manifest with annotations `org.devenes.model.file.path` and `org.devenes.model.file.role`.
2. The OCI registry stores each layer's content-addressable `digest` (`sha256:...`) and `size` in the Subject Manifest's `layers[]` descriptor array.
3. The controller fetches the Subject Manifest (`<= 16 MiB`) and extracts the observed `(path, role, sha256, size)` tuple for every file—including `model.safetensors`—directly from `layers[]` without issuing `GET /v2/<repo>/blobs/<weight-digest>`.
4. The controller then fetches only the small metadata blobs (`bundle-manifest.json`, `oms.sig.json`, `bom.cdx.json`, `evaluation.dsse.json`, `report.json`, and `release-qualification.dsse.json`) via [`Client.FetchMetadataBlob`](../internal/registry/client.go), which explicitly rejects weight media types/roles and wraps the HTTP body in `io.LimitReader(body, 16*1024*1024+1)`.
5. Finally, the verification engine recomputes the RFC 8785 JCS `bundle_id` from the observed OCI layer descriptors and verifies 1:1 file hash correspondence across `bundle-manifest.json`, OMS v1.0 `resources[]`, and CycloneDX 1.7 `components[]`.

---

## 4. Why Three Isolated Cryptographic Trust Domains

In an enterprise AI supply chain, different systems produce different evidence:

1. **`modelSigners`** (Model Training / Packaging Pipeline) — Signs the OpenSSF Model Signing (`openssf-model-signing/v1.0`) envelope over the model files.
2. **`evaluators`** (`llm-release-gate` Evaluation Runner) — Executes behavioral/safety suites and signs `evaluation-evidence/v1` at evaluation time.
3. **`releaseAuthorities`** (`ai-model-provenance-gate` Release Governance Gate) — Verifies all upstream artifacts against release policy and signs `release-qualification/v1`.

If all three roles shared a single pool of trusted public keys, a compromised evaluation runner key (`evaluators`) could be used to sign a backdoored model bundle (`oms.sig.json`) AND approve its own release qualification (`release-qualification.dsse.json`).

By isolating keys into `trustDomains.modelSigners`, `trustDomains.evaluators`, and `trustDomains.releaseAuthorities`—and passing `AllDomainCandidates` into [`pkg/verify`](https://github.com/devenes/ai-model-provenance-gate)—the controller explicitly detects and rejects **cross-domain key confusion** (`UntrustedModelSigner`, `UntrustedEvaluator`, `UntrustedReleaseAuthority`, verified in test cases `N-14`..`N-17`).

---

## 5. Why `policyDigest` Synchronously Invalidates Cached Verifications

Because `ModelArtifact.status.verified` is cached on the Kubernetes API server, what happens when a security administrator rotates a compromised key or tightens a requirement in `ClusterModelTrustPolicy`?

If the admission webhook only checked `modelArtifact.Status.Verified == true`, there would be a race window (until `ModelArtifactReconciler` finishes re-reconciling every `ModelArtifact`) where workloads referencing an old verification would still be admitted under the old policy.

To eliminate this TOCTOU window:

1. [`policy.Resolve`](../internal/policy/evaluator.go) computes a deterministic RFC 8785 JCS SHA-256 digest (`policyDigest`) over all resolved public key fingerprints, allowed registries, requirements, TTLs, and exemptions.
2. Every verified `ModelArtifact` records the `status.policyDigest` under which it was verified.
3. On every admission request, the webhook compares `modelArtifact.Status.PolicyDigest == activePolicy.PolicyDigest` (or synchronously resolves the latest `ClusterModelTrustPolicy` from the API server).
4. The instant a `ClusterModelTrustPolicy` or its referenced `Secret` changes, `activePolicy.PolicyDigest` changes, and the webhook **immediately fails closed** (`PolicyDigestMismatch`) until `ModelArtifactReconciler` completes re-verification against the new policy.

---

## 6. How Multiple Replicas, Leader Election, and Webhook Serving Interact

The Helm chart deploys **2 replicas** of `gke-model-admission-controller` across separate worker nodes with a `PodDisruptionBudget` (`minAvailable: 1`):

- **Webhook Serving (Active-Active across all replicas)**: Every replica runs the HTTPS `ValidatingAdmissionWebhook` server on `:9443` and maintains a synchronized controller-runtime informer cache (`ClusterModelTrustPolicy`, `ModelArtifact`, `Secret`). The Kubernetes `Service` load-balances `AdmissionReview` requests across both replicas. If one replica is terminated, the remaining replica continues serving admission requests with zero downtime (verified in resilience scenario `R-02`).
- **Controller Reconciliation (Leader-Elected)**: `--leader-elect=true` (`gke-model-admission-controller.trust.gke.dev` lease) ensures only one replica actively writes `status` updates to `ClusterModelTrustPolicy` and `ModelArtifact` at a time, preventing concurrent status conflict retries while keeping a hot standby ready to take over the lease on pod restart (`R-01`).
