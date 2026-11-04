# Security Model & Cryptographic Guarantees

## 1. Zero-Trust Supply-Chain Principles

`gke-model-admission-controller` enforces a **fail-closed (`failurePolicy: Fail`)** security posture across all enforced GKE namespaces (`trust.gke.dev/enforce: "true"`):

1. **Default-Deny in Enforced Namespaces**: Every `Pod`, `Deployment`, `StatefulSet`, `DaemonSet`, `Job`, and `CronJob` in an enforced namespace must carry `trust.gke.dev/model-artifact: <name>` (or match an explicit, policy-hashed exemption in `ClusterModelTrustPolicy.spec.exemptions` bound to the authenticated requester `req.UserInfo.Username`). Unannotated workloads are rejected with `MissingModelArtifactReference`.
2. **Immutable Digest Pinning**: Both model artifacts (`ModelArtifact.spec.artifact.uri`) and runtime serving container images (`PodSpec.containers[*].image` and `initContainers[*].image`) must use immutable `@sha256:<64-lowercase-hex>` digests. Mutable tags (`:latest`, `:v1`) and tag+digest combinations (`:v1@sha256:...`) are rejected.
3. **Three Isolated Cryptographic Trust Domains**:
   - `trustDomains.modelSigners`: Authorized to sign OpenSSF Model Signing (`openssf-model-signing/v1.0`) DSSE envelopes.
   - `trustDomains.evaluators`: Authorized to sign native `llm-release-gate` evaluation attestations (`evaluation-evidence/v1`).
   - `trustDomains.releaseAuthorities`: Authorized to sign release qualification attestations (`release-qualification/v1`).
   - **Cross-Domain Key Confusion Prevention**: A valid signature by an Evaluator key on an OMS or Release Qualification envelope is explicitly detected and rejected (`UntrustedModelSigner` / `UntrustedReleaseAuthority`).
4. **Rejection of Post-Hoc Operator Evaluation Binding**: Under strict policy (`requireNativeEvaluatorBinding: true`, `rejectPostHocBinding: true`), only `NATIVE_EVALUATOR` attestations produced at evaluation time by `llm-release-gate run --bundle-manifest ... --signing-key ...` are accepted. `POST_HOC_OPERATOR` (`OPERATOR_BINDING_ENDORSEMENT`) attestations are rejected with `PostHocEvaluationRejected`.
5. **5-Point Cross-Evidence Cryptographic Binding**: The `release-qualification/v1` predicate binds the exact digests of all preceding artifacts (`bundle.manifest_digest`, `bom.digest`, `oms_verification.signature_digest`, `evaluation.report_digest`, `evaluation.attestation_digest`) plus the deployment composition (`runtime_image` and `serving_config_digest`). Tampering with or swapping any individual referrer breaks cross-evidence verification (`CrossEvidenceMismatch`).
6. **LoRA Base-to-Adapter Binding**: When deploying a LoRA adapter, the controller verifies that the adapter's `bundle-manifest.json` and CycloneDX BOM cryptographically declare `verified_base_bundle_id` matching the `status.verifiedBundleID` of a verified base `ModelArtifact` in the same namespace.

---

## 2. GKE Infrastructure & Pod Security Hardening

- **Private Worker Nodes (`enable_private_nodes = true`)**: GKE worker nodes have no public IPv4 addresses. Outbound registry egress uses Cloud NAT.
- **Scoped Webhook Firewall (`CLUSTER_NAME-allow-webhook-9443`)**: Ingress on TCP port `9443` is restricted exclusively to the GKE control plane CIDR (`172.16.0.0/28`) and internal cluster CIDRs (`10.10.0.0/20`, `10.20.0.0/16`). `0.0.0.0/0` ingress is never used.
- **Workload Identity Federation**: The controller authenticates to Google Artifact Registry using GKE Workload Identity (`gke-model-admission-controller@PROJECT_ID.iam.gserviceaccount.com` with `roles/artifactregistry.reader`). No long-lived JSON service account keys exist in the cluster.
- **Hardened Pod & Container SecurityContext**:
  - `runAsNonRoot: true`, `runAsUser: 65532`, `runAsGroup: 65532` (`gcr.io/distroless/static:nonroot`)
  - `readOnlyRootFilesystem: true`
  - `allowPrivilegeEscalation: false`
  - `capabilities.drop: ["ALL"]`
  - `seccompProfile.type: RuntimeDefault`
- **Webhook TLS via `cert-manager`**: Automated X.509 certificate provisioning (`Certificate/gke-model-admission-webhook-cert`) and `caBundle` injection into `ValidatingWebhookConfiguration`.
