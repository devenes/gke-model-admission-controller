# Security Limitations & Non-Guarantees (`LIMITATIONS.md`)

A trustworthy security project must be as explicit about what it **does not** guarantee as what it does. This document explains the architectural boundaries, non-guarantees, and operational considerations of **GKE Model Admission Controller** (`trust.gke.dev/v1alpha1`).

---

## 1. Explicit Non-Guarantees

### 1.1 Valid Provenance Does Not Prove Model Weights Are Benign

A valid OpenSSF Model Signing (OMS v1.0) signature, CycloneDX 1.7 AI/ML-BOM, and Release Qualification attestation prove **cryptographic integrity, origin authenticity, and policy compliance**. They do **not** mathematically prove that a model's weights are free of backdoors, sleeper triggers, data poisoning, or emergent unsafe behaviors not covered by the evaluation dataset.

### 1.2 Passed Behavioral Evaluation Is Bounded by Suite & Dataset Coverage

`llm-release-gate` verifies that a candidate model passed a specific evaluation suite (`expectedSuiteDigest`) and dataset (`expectedDatasetDigest`) without regression. A `PASS` gate status does not guarantee absence of novel jailbreaks, prompt injections, or out-of-distribution failures at serving time.

### 1.3 Admission Verification vs. Application-Level Runtime File Loading

`gke-model-admission-controller` operates at **Kubernetes admission time** (`CREATE` / `UPDATE` of `Pod`, `Deployment`, `StatefulSet`, `DaemonSet`, `Job`, `CronJob`). It verifies that:

- The referenced `ModelArtifact` (`trust.gke.dev/model-artifact`) is cryptographically verified in Google Artifact Registry.
- The Pod's container images match the attested immutable `runtime_image` digest.
- The Pod's `trust.gke.dev/serving-config-digest` annotation matches the attested `serving_config_digest`.

However, if a serving container has unrestricted outbound internet access at runtime and its entrypoint script is programmed to download a different model from Hugging Face or an S3 bucket instead of loading the declared artifact, admission control alone cannot intercept that runtime network call.

**Mitigation**: Pair `gke-model-admission-controller` with Kubernetes `NetworkPolicy`, VPC Service Controls, or an initContainer/CSI volume fetcher that mounts only the digest-pinned `ModelArtifact.spec.artifact.uri` into a read-only volume.

### 1.4 Descriptor-Based Weight Verification Relies on Registry Content-Addressability

Because the controller avoids downloading multi-gigabyte `*.safetensors` blobs (`MaxMetadataBlobBytes = 16 MiB`) and instead verifies `layers[].digest` and `layers[].size` from the OCI v1.1 Subject Manifest against signed `bundle-manifest.json`, OMS v1.0, and CycloneDX 1.7 BOM hashes, it relies on the OCI registry (Google Artifact Registry) verifying layer SHA-256 digests on blob upload and serving the matching blob on pull.

### 1.5 Compromised Trusted Signer Keys

If an attacker compromises the private keys across **all three** isolated trust domains (`modelSigners`, `evaluators`, AND `releaseAuthorities`), the attacker can sign arbitrary artifacts that satisfy cryptographic verification. (Compromising only *one* domain key is insufficient because of 3-domain isolation and 5-point cross-evidence binding.)

**Mitigation**: Store private keys in KMS/HSM in production CI pipelines and rotate `model-trust-keys` promptly using the procedure in [Policy & Key Rotation](operations/policy-rotation.md).

### 1.6 Admission Control Does Not Replace Container Runtime Security or Binary Authorization

- **Binary Authorization** verifies the provenance and vulnerability attestations of **container images**.
- **GKE Model Admission Controller** verifies the provenance, AI/ML-BOM, behavioral evaluation, LoRA base binding, and release qualification of **AI model bundles**.
- They solve complementary halves of the AI deployment problem and should be used together alongside GKE Sandbox (gVisor), Pod Security Standards (`restricted`), and runtime monitoring.

---

## 2. `DEMO-READY` vs. `PRODUCTION-READY`

| Dimension | Current State in Repository (`v0.1.0`) | Additional Steps for Enterprise Production |
|---|---|---|
| **GKE Control Plane & Topology** | Zonal GKE Standard cluster (`us-west4-a`) with `2 x e2-standard-4` private CPU nodes (`DEMO-READY` & cost-optimized). | Upgrade to a **Regional GKE cluster** across 3 zones (`node_locations`) with cluster autoscaling and dedicated GPU/TPU serving node pools. |
| **Webhook TLS Certificates** | Self-signed cluster-internal `Issuer` managed by `cert-manager` with automatic `caBundle` injection. | Optionally integrate with Google Cloud Certificate Authority Service (CAS) or organization PKI. |
| **Signing Key Storage** | Ed25519 PEM keys generated via `ai-model-provenance keygen` and public keys stored in Kubernetes `Secret/model-trust-keys`. | Store private signing keys in **Google Cloud KMS** / Hardware Security Modules (HSM) inside isolated CI/CD runners. |
| **Namespace Enforcement Scope** | Opt-in via namespace label `trust.gke.dev/enforce: "true"` (leaving `kube-system` and `model-admission-system` excluded). | Enforce namespace label governance via Gatekeeper/Kyverno or organization-wide namespace provisioning templates. |
| **Regulatory / Compliance Status** | Open-source reference implementation of cryptographic AI model admission control. | Not a formal compliance certification (e.g., SOC 2, ISO 42001, or EU AI Act certification). |
