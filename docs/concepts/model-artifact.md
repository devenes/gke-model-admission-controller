# Concept: `ModelArtifact` Custom Resource (`trust.gke.dev/v1alpha1`)

The `ModelArtifact` CustomResourceDefinition ([`config/crd/trust.gke.dev_modelartifacts.yaml`](../../config/crd/trust.gke.dev_modelartifacts.yaml)) is a namespaced Kubernetes resource that declares the immutable cryptographic identity of an AI model bundle and records its verified supply-chain state.

---

## 1. Why `ModelArtifact` Exists

In Kubernetes, a `Deployment` or `Pod` specifies container images, volumes, and environment variables, but has no native concept of an AI model bundle, its `bundle-identity/v1` digest, its CycloneDX AI/ML-BOM, or its behavioral evaluation status.

`ModelArtifact` bridges this gap:

1. **Pre-Deployment Verification**: Platform or ML engineers create a `ModelArtifact` in the target serving namespace (`ai-serving`). `ModelArtifactReconciler` asynchronously verifies all 4 OCI v1.1 referrers in Google Artifact Registry and writes the verification result to `ModelArtifact.status`.
2. **Spec Immutability via CEL**: The CRD enforces `x-kubernetes-validations: [{rule: "self == oldSelf", message: "ModelArtifact.spec is immutable; create a new versioned ModelArtifact instead"}]`. To deploy a new model version, teams create a new `ModelArtifact` (or replace it), preventing in-place mutation of a running model's identity.
3. **Zero-I/O Admission Lookup**: Workloads reference the `ModelArtifact` via the annotation `trust.gke.dev/model-artifact: <name>`, allowing the admission webhook to validate the model in `<50ms` from its informer cache.

---

## 2. Schema Reference (`ModelArtifactSpec` & `ModelArtifactStatus`)

```yaml
apiVersion: trust.gke.dev/v1alpha1
kind: ModelArtifact
metadata:
  name: gemma-2b-it
  namespace: ai-serving
spec:
  policyName: default
  artifact:
    # Must be a digest-pinned OCI reference (@sha256:<64-lowercase-hex>)
    uri: us-west4-docker.pkg.dev/PROJECT_ID/models/gemma-2b-it@sha256:023c766a84b1dcfb402d977fdff2efcd005857a7fc68e60ddbbf427007779770
  identity:
    # Expected deterministic bundle-identity/v1 digest
    bundleID: sha256:bfb3d85c2a3279bf77b8500008acb14036b31850bc1df89d9867679ffc883669
  runtime:
    # Expected digest-pinned serving container image
    runtimeImage: us-west4-docker.pkg.dev/PROJECT_ID/models/gke-model-admission-controller@sha256:9ca52f2367a58106b6081085e6d3c9020409e03dbc34e21155f06600e8d83c93
    # Expected SHA-256 digest of the serving configuration
    servingConfigDigest: sha256:5555555555555555555555555555555555555555555555555555555555555555
    # Optional: name of verified base ModelArtifact in the same namespace (for LoRA adapters)
    baseModelArtifact: ""
```

---

## 3. Seven Status Conditions

`ModelArtifact.status.conditions` reports granular progress across all 7 verification stages:

| Condition Type | Meaning When `status: "True"` |
|---|---|
| `ArtifactResolved` | OCI Subject Manifest fetched, digest verified, and layer descriptors extracted without downloading weight blobs. |
| `BundleIdentityVerified` | `bundle-manifest.json` config blob verified; JCS `bundle_id` recomputed from OCI layer descriptors matches `spec.identity.bundleID`. |
| `OMSSignatureVerified` | OpenSSF Model Signing (`openssf-model-signing/v1.0`) referrer verified against `trustDomains.modelSigners` and 1:1 file digests. |
| `BOMVerified` | CycloneDX 1.7 AI/ML-BOM referrer validated against schema, root `bundle_id`, and 1:1 component hashes. |
| `EvaluationVerified` | Native `llm-release-gate` DSSE attestation (`NATIVE_EVALUATOR`) and `report.json` verified against `trustDomains.evaluators` with `gateStatus == "PASS"`. |
| `ReleaseQualificationPassed` | `release-qualification/v1` referrer verified against `trustDomains.releaseAuthorities` with 5-point cross-evidence digest equality. |
| `Ready` | Overall summary condition (`status.verified == true`, `status.policyDigest` recorded, `status.validUntil` set). |
