# Concept: Three Isolated Cryptographic Trust Domains

`ClusterModelTrustPolicy` separates signing authority into three mutually exclusive cryptographic trust domains:

1. **`trustDomains.modelSigners`** — Model packaging / build pipeline keys (`oms.sig.json`).
2. **`trustDomains.evaluators`** — `llm-release-gate` behavioral evaluation runner keys (`evaluation.dsse.json`).
3. **`trustDomains.releaseAuthorities`** — Release governance gate keys (`release-qualification.dsse.json`).

---

## 1. Principal Definition (`TrustedPrincipalSpec`)

Each principal in a trust domain is configured with:

```yaml
- id: model-signer-1
  keyID: ms-1
  keyFingerprint: sha256:8844fb38f2944210c95ec05a5616f6824f891feae89e50efbac486a57e9d260e
  secretRef:
    namespace: model-admission-system
    name: model-trust-keys
    key: model-signer.pub
```

- **`id`**: Unique identifier for audit logs and `ModelArtifact.status`.
- **`keyID`**: Expected `keyid` in the DSSE signature header.
- **`keyFingerprint`**: `sha256:<64-lowercase-hex>` over the DER-encoded PKIX SubjectPublicKeyInfo bytes.
- **`secretRef` or `publicKeyPEM`**: Source of the PEM-encoded Ed25519 public key.

---

## 2. Why Single-Domain Compromise Cannot Approve a Model

| If an Attacker Compromises Only... | Can They Forge OMS Signature? | Can They Forge Native Eval Attestation? | Can They Forge Release Qualification? | Result at GKE Admission |
|---|---|---|---|---|
| `modelSigners` key | Yes | **No** (`UntrustedEvaluator`, `N-15`) | **No** (`UntrustedReleaseAuthority`, `N-16`) | **DENY** |
| `evaluators` key | **No** (`UntrustedModelSigner`, `N-17`) | Yes | **No** (`UntrustedReleaseAuthority`, `N-16`) | **DENY** |
| `releaseAuthorities` key | **No** (`UntrustedModelSigner`, `N-14`) | **No** (`UntrustedEvaluator`, `N-15`) | Yes (but Stage 4 & Stage 6 fail) | **DENY** |
