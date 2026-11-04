# Threat Model & Adversarial Matrix

This document defines the threat model, trust boundaries, attacker assumptions, and the 28-scenario adversarial test matrix verified against **GKE Model Admission Controller**.

---

## 1. Attacker Capabilities Assumed in Scope

We assume an adversary (or compromised CI/CD job / misconfigured developer workflow) may possess one or more of the following capabilities:

1. **Registry Push Access**: Can push modified `.safetensors` weights, altered `tokenizer.json` / `config.json` files, or forged OCI referrers to Artifact Registry.
2. **Partial Key Compromise (Single Trust Domain)**: Compromises **one** signing key (for example, the `modelSigners` key in the model training pipeline) and attempts to forge evaluation evidence (`evaluation-evidence/v1`) or release qualification (`release-qualification/v1`).
3. **Evidence Replay / Transplanting**: Takes a valid `evaluation.dsse.json` or `bom.cdx.json` from a passing model `Model-A` and attaches it as an OCI referrer to `Model-B`.
4. **Kubernetes Workload Deployment Privileges**: Has RBAC permissions to create or update `Pods`, `Deployments`, `StatefulSets`, `DaemonSets`, `Jobs`, or `CronJobs` in `ai-serving` and attempts to:
   - Omit the `trust.gke.dev/model-artifact` annotation.
   - Self-assign `trust.gke.dev/exempt: "true"`.
   - Swap the approved `vllm` container image for a mutable `:latest` tag or an unapproved image.
   - Change the serving configuration digest or pair a LoRA adapter with an unverified base model.

---

## 2. 28-Scenario Adversarial Verification Matrix (`N-01` – `N-28`)

Every threat below is continuously tested both in local unit tests ([`tests/security_matrix_test.go`](../../tests/security_matrix_test.go)) and against the live GKE cluster ([`cmd/gke-e2e/main.go`](../../cmd/gke-e2e/main.go)):

| ID | Threat / Attack Scenario | Enforced Layer | Expected Failure Reason |
|---|---|---|---|
| **N-01** | Unsigned model bundle (missing `oms.sig.json` OCI referrer) | Reconciler | `OCI_RESOLUTION_FAILED` |
| **N-02** | Tampered model weight digest in OCI layer vs. `bundle-manifest.json` | Reconciler (`pkg/verify`) | `BUNDLE_ID_MISMATCH` |
| **N-03** | OMS signature signed by an unknown key | Reconciler (`pkg/verify`) | `OMS_SIGNATURE_INVALID` |
| **N-04** | Cross-domain key reuse: OMS signature signed by an `evaluatorSigners` key | Reconciler (`pkg/verify`) | `OMS_SIGNATURE_INVALID` (`UNTRUSTED`) |
| **N-05** | Missing CycloneDX AI/ML-BOM referrer | Reconciler | `OCI_RESOLUTION_FAILED` |
| **N-06** | Downgraded CycloneDX BOM (`specVersion: "1.5"` instead of `"1.7"`) | Reconciler (`pkg/verify`) | `BOM_INVALID` |
| **N-07** | BOM component hash mismatch against model layers | Reconciler (`pkg/verify`) | `BOM_INVALID` |
| **N-08** | Missing `evaluation.dsse.json` OCI referrer | Reconciler | `OCI_RESOLUTION_FAILED` |
| **N-09** | Evaluation report with `gate_status: FAIL` | Reconciler (`pkg/verify`) | `EVALUATION_INVALID` |
| **N-10** | Post-hoc evaluation binding (`binding_mode: POST_HOC_OPERATOR`) | Reconciler (`pkg/verify`) | `EVALUATION_INVALID` |
| **N-11** | Evaluation attestation signed by `modelSigners` key (cross-domain attack) | Reconciler (`pkg/verify`) | `EVALUATION_INVALID` |
| **N-12** | Evaluation suite digest mismatch (`expectedSuiteDigest`) | Reconciler (`pkg/verify`) | `EVALUATION_INVALID` |
| **N-13** | Evaluation dataset digest mismatch (`expectedDatasetDigest`) | Reconciler (`pkg/verify`) | `EVALUATION_INVALID` |
| **N-14** | Expired evaluation attestation (`timestamp` older than `maxAgeHours`) | Reconciler (`pkg/verify`) | `EVALUATION_INVALID` |
| **N-15** | Missing `release-qualification.dsse.json` OCI referrer | Reconciler | `OCI_RESOLUTION_FAILED` |
| **N-16** | Release qualification signed by `evaluatorSigners` key (cross-domain attack) | Reconciler (`pkg/verify`) | `RELEASE_QUALIFICATION_INVALID` |
| **N-17** | Evidence transplanting: valid `release-qualification.dsse.json` whose pinned `bom.digest` does not match the sibling `bom.cdx.json` referrer | Reconciler (`pkg/verify`) | `RELEASE_QUALIFICATION_INVALID` |
| **N-18** | Unsafe `.pkl` / pickle model format (`rejectUnsafePickle: true`) | Reconciler (`internal/policy`) | `FORMAT_FORBIDDEN` / `UNSAFE_PICKLE_DETECTED` |
| **N-19** | Remote code execution hooks (`auto_map` in `config.json`) | Reconciler (`internal/policy`) | `REMOTE_CODE_HOOKS_DETECTED` |
| **N-20** | LoRA adapter referencing a `Failed` base model `ModelArtifact` | Reconciler + Webhook | `BASE_MODEL_UNVERIFIED` / `BASE_MODEL_NOT_VERIFIED` |
| **N-21** | LoRA adapter whose attested `baseModelBundleId` mismatches parent `ModelArtifact.spec.bundleIdentity` | Reconciler + Webhook | `LORA_BASE_MISMATCH` |
| **N-22** | `ModelArtifact.spec.storageUri` using a mutable tag (`:latest`) instead of `@sha256:` | Reconciler (`internal/policy`) | `STORAGE_URI_NOT_DIGEST_PINNED` |
| **N-23** | Serving workload container image using a mutable tag (`:latest`) instead of `@sha256:` | Webhook | `RUNTIME_IMAGE_NOT_DIGEST_PINNED` |
| **N-24** | Serving workload container image digest differs from `ModelArtifact.spec.runtimeImage` | Webhook | `RUNTIME_IMAGE_MISMATCH` |
| **N-25** | Serving workload `trust.gke.dev/serving-config-digest` differs from `ModelArtifact.spec.servingConfigDigest` | Webhook | `SERVING_CONFIG_DIGEST_MISMATCH` |
| **N-26** | Model-serving workload (`vllm` / `--model`) in protected namespace omitting `trust.gke.dev/model-artifact` | Webhook | `MISSING_MODEL_ARTIFACT_REF` |
| **N-27** | Unauthorized workload creator adding `trust.gke.dev/exempt: "true"` | Webhook | `UNAUTHORIZED_EXEMPTION` |
| **N-28** | Workload referencing a `ModelArtifact` in a different namespace | Webhook | `MODEL_ARTIFACT_NOT_FOUND` |
