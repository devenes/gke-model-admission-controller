# Security Guarantees (`SECURITY_GUARANTEES.md`)

This document states the exact, implementation-verified cryptographic and operational security guarantees provided by **GKE Model Admission Controller** (`trust.gke.dev/v1alpha1`) when `ClusterModelTrustPolicy` is configured in `Enforce` mode.

Every guarantee listed below is backed by concrete enforcement logic in [`internal/verification/verifier.go`](../internal/verification/verifier.go), [`internal/admission/validator.go`](../internal/admission/validator.go), and [`internal/policy/evaluator.go`](../internal/policy/evaluator.go) and is continuously verified by the 28-scenario negative security matrix ([`tests/security_matrix_test.go`](../tests/security_matrix_test.go)).

---

## 1. Summary of Verified Guarantees

| Guarantee | Enforcement Mechanism | Verified By |
|---|---|---|
| **1. Default-Deny in Enforced Namespaces** | Any `Pod`, `Deployment`, `StatefulSet`, `DaemonSet`, `Job`, or `CronJob` in a namespace labeled `trust.gke.dev/enforce: "true"` without `trust.gke.dev/model-artifact` is rejected. | `N-01`, `P-01`..`P-06` |
| **2. Immutable OCI Artifact & Runtime Image Digest Pinning** | Both `ModelArtifact.spec.artifact.uri` and all workload container/initContainer images must use `@sha256:<64-lowercase-hex>`. Mutable tags (`:latest`, `:v1`) and tag+digest combinations (`:v1@sha256:...`) are rejected. | `N-03`, `N-24` |
| **3. Registry Allowlist Enforcement** | Only OCI URIs matching a prefix in `ClusterModelTrustPolicy.spec.allowedRegistries` are reconciled; external registries are rejected before any HTTP call. | `N-04` |
| **4. Deterministic Model Bundle Identity (`bundle-identity/v1`)** | The controller recomputes the RFC 8785 JCS `bundle_id` from observed OCI layer descriptors (`path`, `role`, `sha256`, `size`) and verifies exact equality with `ModelArtifact.spec.identity.bundleID` and `bundle-manifest.json`. | `N-10`, `N-11`, `N-12`, `N-13` |
| **5. OpenSSF Model Signing (OMS v1.0) Integrity & Signer Trust** | Requires a single unambiguous OMS v1.0 referrer whose `subject.digest` matches the OCI Subject Manifest, whose Ed25519 signature verifies against `trustDomains.modelSigners`, and whose per-file digests match every OCI layer descriptor 1:1. | `N-05`, `N-08`, `N-14` |
| **6. CycloneDX 1.7 AI/ML-BOM Completeness** | Requires a schema-valid CycloneDX 1.7 BOM referrer whose root component hash matches `bundle_id` and whose file component hashes match the OCI layer descriptors 1:1. | `N-06` |
| **7. Native Evaluation-Time Binding (`llm-release-gate`)** | Requires a dual-layer evaluation referrer (`evaluation.dsse.json` + `report.json`) signed by `trustDomains.evaluators` with `gateStatus == "PASS"`, `evidenceQuality == "COMPLETE"`, `bindingMode == "NATIVE_EVALUATOR"`, matching `expectedSuiteDigest` and `expectedDatasetDigest`, and age within `maxEvidenceAgeHours`. Post-hoc operator bindings (`POST_HOC_OPERATOR`) are rejected. | `N-07`, `N-15`, `N-18`, `N-19`, `N-20`, `N-21`, `N-22` |
| **8. 5-Point Release Qualification Cross-Evidence Binding** | Requires a `release-qualification/v1` referrer signed by `trustDomains.releaseAuthorities` that cryptographically binds the exact digests of `bundle.manifest_digest`, `bom.digest`, `oms_verification.signature_digest`, `evaluation.report_digest`, and `evaluation.attestation_digest`. | `N-09`, `N-16`, `N-23` |
| **9. Strict Isolation Across Three Signer Trust Domains** | A valid signature produced by a key in `evaluators` or `releaseAuthorities` is explicitly rejected when presented on an OMS signature, and vice versa (`AllDomainCandidates` cross-domain key confusion detection). | `N-17` |
| **10. Runtime Container Image & Serving Config Binding** | The admission webhook verifies that every workload container image matches the attested `runtime_image` digest and that `trust.gke.dev/serving-config-digest` matches the attested `serving_config_digest`. | `N-25`, `N-26` |
| **11. LoRA Adapter-to-Base-Model Cryptographic Binding** | When a `ModelArtifact` is a LoRA adapter (`has_lora_adapter: true`), both the reconciler and webhook verify that `spec.runtime.baseModelArtifact` points to a verified base `ModelArtifact` in the same namespace whose `status.verifiedBundleID` matches the adapter's attested `verified_base_bundle_id`. | `P-07`, `N-27` |
| **12. Synchronous Policy Digest & TTL Freshness Enforcement** | On every admission request, the webhook verifies `status.observedGeneration == metadata.generation`, `status.policyDigest == activePolicy.PolicyDigest`, and `now < status.validUntil`. Key rotation or policy updates immediately invalidate stale verifications. | `N-28`, `R-03` |
| **13. Non-Self-Assignable Break-Glass Exemptions** | Admission exemptions require the Kubernetes API-server-authenticated requester (`req.UserInfo.Username`) to match `exemptServiceAccounts` AND Pod labels to match `exemptPodLabels`. Workload creators cannot self-assign an exemption via YAML labels or `spec.serviceAccountName`. | `P-10` |
| **14. Bounded Memory / Zero Model Weight Downloads** | All OCI blob reads are restricted to metadata roles/media types and capped at `MaxMetadataBlobBytes = 16 MiB` via `io.LimitReader`. Model weight blobs (`*.safetensors`) are never downloaded by the controller. | `TestEndToEndZeroWeightDownloadAndAllSixWorkloadKinds` |

---

## 2. Related Documents

- **[Security Limitations & Non-Guarantees](LIMITATIONS.md)** — What this system does *not* guarantee and why defense-in-depth is required.
- **[Threat Model (`TM-01` .. `TM-12`)](THREAT_MODEL.md)** — Full mapping of threat actors and attack vectors.
