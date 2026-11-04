# Troubleshooting Guide

This guide lists every deterministic failure code emitted by the `ModelArtifact` reconciler and the `ValidatingAdmissionWebhook`, its root cause, and how to resolve it.

---

## 1. Admission Webhook Denial Codes (`internal/webhook/admission.go`)

| Denial Code | Cause | Resolution |
|---|---|---|
| `NO_TRUST_POLICY` | `ClusterModelTrustPolicy/default` does not exist in the cluster | Apply `ClusterModelTrustPolicy` with `metadata.name: default` |
| `POLICY_NOT_READY` | `ClusterModelTrustPolicy/default` has invalid PEM keys or has not finished reconciling (`status.observedGeneration != metadata.generation`) | Run `kubectl get clustermodeltrustpolicy default -o yaml` and inspect `status.conditions` |
| `UNAUTHORIZED_EXEMPTION` | Workload carries `trust.gke.dev/exempt: "true"`, but the API caller (`req.UserInfo.Username`) is not listed in `spec.exemptions.allowedServiceAccounts` | Remove `trust.gke.dev/exempt` or authorize the specific ServiceAccount in `ClusterModelTrustPolicy` |
| `MISSING_MODEL_ARTIFACT_REF` | Workload in a protected namespace (`model-admission.gke.io/enforce: "true"`) runs a model-serving container (`vllm`, `tgi`, `tritonserver`, `ollama`, `--model`, or `/models` mount) or carries model labels without `trust.gke.dev/model-artifact` | Add `trust.gke.dev/model-artifact: <verified-model-name>` to the workload metadata and Pod template annotations |
| `MODEL_ARTIFACT_NOT_FOUND` | The referenced `ModelArtifact` does not exist in the workload's namespace (`req.Namespace`) | Create the `ModelArtifact` in the **same** namespace as the workload (cross-namespace references are forbidden) |
| `GENERATION_UNOBSERVED` | `ModelArtifact.metadata.generation != ModelArtifact.status.observedGeneration` | Wait 1–2 seconds for reconciliation or check controller logs if reconciliation is stuck |
| `MODEL_ARTIFACT_NOT_VERIFIED` | `ModelArtifact.status.phase` is `Failed` or `Pending` | Inspect `kubectl get modelartifact <name> -n <ns> -o yaml` and check `status.failureReason` and `status.failureMessage` |
| `POLICY_GENERATION_STALE` | `ClusterModelTrustPolicy/default` was updated, and the `ModelArtifact` has not finished re-verifying against the new generation | Wait for automatic re-reconciliation or inspect why the `ModelArtifact` failed under the new policy |
| `POLICY_DIGEST_MISMATCH` | `ModelArtifact.status.evaluatedPolicyDigest != policy.status.policyDigest` | The policy public keys or spec changed; wait for controller re-reconciliation |
| `ATTESTATION_EXPIRED` | Current time exceeds `ModelArtifact.status.validUntil` | Re-run evaluation and release qualification (`llm-release-gate` + `ai-model-provenance gate evaluate`), push updated referrers, or increase `maxAgeHours` |
| `BASE_MODEL_NOT_FOUND` | LoRA `ModelArtifact.spec.baseModelRef.name` does not exist in the namespace | Register and verify the base foundation `ModelArtifact` in the same namespace |
| `BASE_MODEL_NOT_VERIFIED` | The parent base `ModelArtifact` is in `Failed` or `Pending` phase | Fix the parent base `ModelArtifact` first |
| `LORA_BASE_MISMATCH` | LoRA attestation `baseModelBundleId` does not match `baseModel.spec.bundleIdentity` | Re-qualify the LoRA adapter against the exact `bundle-manifest.json` of the deployed base model |
| `RUNTIME_IMAGE_NOT_DIGEST_PINNED` | Serving container image uses a tag (`:latest`, `:v1`) instead of `@sha256:<64-hex>` | Pin the container image by its `@sha256:...` digest |
| `RUNTIME_IMAGE_MISMATCH` | Serving container image does not match `ModelArtifact.spec.runtimeImage` | Update the Deployment container `image:` to match the qualified `runtimeImage` |
| `RUNTIME_IMAGE_FORBIDDEN` | Serving container image does not match any pattern in `ClusterModelTrustPolicy.spec.releaseRequirements.allowedRuntimeImages` | Use an approved runtime image repository or update `allowedRuntimeImages` |
| `SERVING_CONFIG_DIGEST_MISMATCH` | `trust.gke.dev/serving-config-digest` annotation is missing or does not match `ModelArtifact.spec.servingConfigDigest` | Set `trust.gke.dev/serving-config-digest` to the exact `sha256:<64-hex>` of the qualified serving configuration |

---

## 2. `ModelArtifact` Reconciliation Failure Codes (`internal/controller/` & `internal/policy/`)

| Failure Code (`status.failureReason`) | Cause | Resolution |
|---|---|---|
| `STORAGE_URI_NOT_DIGEST_PINNED` | `ModelArtifact.spec.storageUri` does not end with `@sha256:<64-hex>` | Pin `storageUri` to the immutable OCI model manifest digest |
| `FORMAT_FORBIDDEN` | `spec.modelFormat` or `bundle-manifest.json` `primary_format` is not in `allowedModelFormats` (e.g. `pickle` or `gguf`) | Convert weights to `safetensors` or update `allowedModelFormats` |
| `OCI_RESOLUTION_FAILED` | Registry unreachable, manifest digest not found, missing `bundle-manifest.json` config, or missing one of the 4 required OCI v1.1 referrers | Check Artifact Registry IAM (`roles/artifactregistry.reader`) and verify all 4 referrers were pushed via `modelctl push` |
| `IDENTITY_COMPUTATION_FAILED` | OCI layer descriptors are missing `org.opencontainers.image.title` or contain invalid relative paths | Repackage using `modelctl push` |
| `BUNDLE_ID_MISMATCH` | Recomputed `bundle-identity/v1` from OCI layer descriptors does not match `ModelArtifact.spec.bundleIdentity` | A weight/config layer digest was modified or `spec.bundleIdentity` has a typo |
| `MANIFEST_INVALID` | `bundle-manifest.json` fails RFC 8785 JCS `manifest_digest` check or mismatches OCI layer inventory | Regenerate `bundle-manifest.json` via `ai-model-provenance inspect` |
| `UNSAFE_PICKLE_DETECTED` | Bundle contains `.pkl`, `.pt`, `.bin`, or pickle payloads while `rejectUnsafePickle: true` | Remove pickle artifacts and use pure `.safetensors` |
| `REMOTE_CODE_HOOKS_DETECTED` | Bundle `config.json` contains `auto_map` or custom `.py` execution hooks while `rejectRemoteCodeHooks: true` | Remove remote code hooks from the model bundle |
| `OMS_SIGNATURE_INVALID` | `oms.sig.json` DSSE signature is invalid, signed by an untrusted/cross-domain key, or its file inventory digests mismatch the OCI layers | Re-sign the bundle with a key listed in `spec.modelSigners` |
| `BOM_INVALID` | `bom.cdx.json` is not valid CycloneDX 1.7, misses model card metadata, or its file hashes mismatch the bundle | Regenerate `bom.cdx.json` using `ai-model-provenance bom generate` |
| `EVALUATION_INVALID` | `evaluation.dsse.json` signature is invalid/untrusted, `gate_status` is `FAIL`, binding mode is `POST_HOC_OPERATOR`, suite/dataset digests mismatch policy, or attestation is expired | Run `llm-release-gate run` with `--bundle-manifest` and `--signing-key` using an authorized `evaluatorSigners` key |
| `RELEASE_QUALIFICATION_INVALID` | `release-qualification.dsse.json` signature is invalid/untrusted, expired, or its pinned child digests (`manifest_digest`, `bom.digest`, `oms_verification.signature_digest`, `evaluation.report_digest`, `evaluation.attestation_digest`) do not match the sibling OCI referrers | Re-run `ai-model-provenance gate evaluate` and push the matching referrers together |
| `RUNTIME_IMAGE_NOT_DIGEST_PINNED` | `ReleaseQualification` or `ModelArtifact` `runtimeImage` is not `@sha256:` pinned | Qualify with `--runtime-image ...@sha256:...` |
| `RUNTIME_IMAGE_MISMATCH` | `ModelArtifact.spec.runtimeImage` differs from the `runtime_image` inside `release-qualification.dsse.json` | Align `ModelArtifact.spec.runtimeImage` with the signed release qualification predicate |
| `SERVING_CONFIG_DIGEST_MISMATCH` | `ModelArtifact.spec.servingConfigDigest` differs from `serving_config_digest` inside `release-qualification.dsse.json` | Align `ModelArtifact.spec.servingConfigDigest` with the signed release qualification predicate |
