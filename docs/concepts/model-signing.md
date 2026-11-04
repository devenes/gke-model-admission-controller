# Concept: OpenSSF Model Signing (`openssf-model-signing/v1.0`)

`gke-model-admission-controller` verifies **OpenSSF Model Signing v1.0** (`https://model_signing/signature/v1.0`) signatures over model bundles to ensure artifact authenticity and per-file integrity.

---

## 1. Structure of an OMS v1.0 Signature

When `ai-model-provenance oms sign` signs a model bundle, it produces a DSSE envelope (optionally wrapped in a Sigstore v0.3 JSON bundle) containing an in-toto Statement v1 (`https://in-toto.io/Statement/v1`):

- **Payload Type**: `application/vnd.in-toto+json`
- **Predicate Type**: `https://model_signing/signature/v1.0`
- **Subject Digest (`oms_root_digest`)**: The SHA-256 digest over the concatenated raw 32-byte SHA-256 digests of the sorted model files.
- **Predicate Resources (`predicate.resources[]`)**: Every file in the model bundle with its relative `name`, `algorithm: "SHA256"`, and hex `digest`.
- **Cryptographic Signature**: Ed25519 signature over the DSSE Pre-Authentication Encoding (PAE) produced by a key in `trustDomains.modelSigners`.

---

## 2. How the Controller Verifies OMS v1.0 Without Downloading Weights

In Stage 4 of [`internal/verification/verifier.go`](../../internal/verification/verifier.go):

1. The controller queries `GET /v2/<repo>/referrers/<subject-digest>` and filters for `artifactType: application/vnd.openssf.model-signing.signature.v1+json`.
2. If zero or more than one unpinned OMS referrer is returned, verification fails closed (`OMSSignatureInvalid`, tested in `N-05` and `N-08`).
3. The controller downloads the small OMS signature blob (`< 16 KiB`) and invokes `verify.VerifyOMSBytes(rawOMS, descriptors, ...)`.
4. `VerifyOMSBytes` verifies:
   - The DSSE Ed25519 signature against `trustDomains.modelSigners` (rejecting keys from `evaluators` or `releaseAuthorities` via `UntrustedModelSigner`, tested in `N-14` and `N-17`).
   - Exact 1:1 correspondence between every OCI layer descriptor `(path, sha256)` and `predicate.resources[]` (detecting any tampered weight, config, or tokenizer layer, or any extra undeclared layer).
   - Recomputation of `oms_root_digest` matching `subject[0].digest.sha256`.
