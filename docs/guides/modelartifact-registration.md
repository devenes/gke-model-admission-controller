# `ModelArtifact` Registration & Reconciliation Guide

A `ModelArtifact` (`trust.gke.dev/v1alpha1`) is a namespaced CustomResourceDefinition representing a versioned AI model bundle or LoRA adapter stored in an OCI v1.1 registry.

---

## 1. Registering a Foundation Model (`ModelArtifact`)

Create a `ModelArtifact` in the target namespace (e.g. `ai-serving`) where your inference workloads will run:

```yaml
apiVersion: trust.gke.dev/v1alpha1
kind: ModelArtifact
metadata:
  name: gemma-2b-it
  namespace: ai-serving
spec:
  storageUri: oci://us-west4-docker.pkg.dev/PROJECT_ID/models/gemma-2b-it@sha256:6267e4f82361f0de6fe473de80ab6db8ff42ff73ba8f5c11a4f788bf375c39c6
  bundleIdentity: sha256:07c70fa3dd5bfee95197e2acfc9596559d143118b07f571b9fa5f71c4316a5fd
  modelFormat: safetensors
  runtimeClass: vllm
  runtimeImage: us-west4-docker.pkg.dev/PROJECT_ID/models/vllm-runtime@sha256:1111111111111111111111111111111111111111111111111111111111111111
  servingConfigDigest: sha256:c1fb3dd4b72e479221aa59c0b5ff33d4eae61a14c2de7ffdd3a6cf57054f34b8
```

### Mandatory `spec` Invariants

1. **`spec.storageUri` MUST be digest-pinned (`@sha256:<64-hex>`)**:
   - Mutable tags (`:latest`, `:v1.0`) are rejected immediately (`STORAGE_URI_NOT_DIGEST_PINNED`).
2. **`spec.bundleIdentity` MUST match the deterministic `bundle-identity/v1` digest (`sha256:<64-hex>`)**:
   - Recomputed by the controller from the OCI model manifest's `layers[]` descriptors without downloading model weights.

---

## 2. Registering a LoRA Adapter (`spec.baseModelRef`)

When registering a LoRA adapter, reference its Verified base model in the same namespace using `spec.baseModelRef`:

```yaml
apiVersion: trust.gke.dev/v1alpha1
kind: ModelArtifact
metadata:
  name: gemma-2b-lora
  namespace: ai-serving
spec:
  storageUri: oci://us-west4-docker.pkg.dev/PROJECT_ID/models/gemma-2b-lora@sha256:bd8cd813a436270e5e44d563eb06b7638c764e36bd46df7ff6a227b69000f5aa
  bundleIdentity: sha256:f687a68dc60f4666c16daaa78cf1bf5bd1e1fd06f7aaaf25a56dd61139ed3dd3
  modelFormat: safetensors
  runtimeClass: vllm
  runtimeImage: us-west4-docker.pkg.dev/PROJECT_ID/models/vllm-runtime@sha256:1111111111111111111111111111111111111111111111111111111111111111
  servingConfigDigest: sha256:c1fb3dd4b72e479221aa59c0b5ff33d4eae61a14c2de7ffdd3a6cf57054f34b8
  baseModelRef:
    name: gemma-2b-it
```

---

## 3. Inspecting Verification Status

Apply and inspect the `ModelArtifact`:

```bash
kubectl apply -f modelartifact.yaml
kubectl get modelartifacts -n ai-serving
```

Example output:

```text
NAME            PHASE      BUNDLEIDENTITY                                                           RUNTIME   POLICYDIGEST
gemma-2b-it     Verified   sha256:07c70fa3dd5bfee95197e2acfc9596559d143118b07f571b9fa5f71c4316a5fd  vllm      sha256:322cb55cf42416d9e9b1793f9dbcd1839c7ccdf5be76898119b1e711f0dbefaf
gemma-2b-lora   Verified   sha256:f687a68dc60f4666c16daaa78cf1bf5bd1e1fd06f7aaaf25a56dd61139ed3dd3  vllm      sha256:322cb55cf42416d9e9b1793f9dbcd1839c7ccdf5be76898119b1e711f0dbefaf
```

### Detailed Status Fields (`kubectl get modelartifact gemma-2b-it -n ai-serving -o yaml`)

| Field | Meaning |
|---|---|
| `status.phase` | `Pending`, `Verified`, or `Failed` |
| `status.observedGeneration` | Must equal `metadata.generation` for the admission webhook to trust the status |
| `status.policyGeneration` | Generation of `ClusterModelTrustPolicy/default` evaluated during reconciliation |
| `status.evaluatedPolicyDigest` | Exact `sha256:<64-hex>` JCS digest of `ClusterModelTrustPolicy/default` |
| `status.resolvedManifestDigest` | Resolved OCI manifest digest (`sha256:<64-hex>`) |
| `status.verifiedBundleIdentity` | Recomputed `bundle-identity/v1` digest from OCI layer descriptors |
| `status.omsStatus` / `omsSigner` | OpenSSF Model Signing v1.0 verification outcome and signer ID |
| `status.bomStatus` / `bomDigest` | CycloneDX 1.7 AI/ML-BOM verification outcome and RFC 8785 JCS digest |
| `status.evalStatus` / `evalGateStatus` | `llm-release-gate` verification outcome (`PASS` / `FAIL`) |
| `status.releaseStatus` / `releaseAttestationDigest` | Release Qualification verification outcome and DSSE digest |
| `status.validUntil` | Expiration timestamp computed from `min(evalTimestamp + evalMaxAge, rqTimestamp + rqMaxAge)` |
| `status.failureReason` / `failureMessage` | Populated with a deterministic reason code when `status.phase == Failed` |
