# Concept: in-toto Statement v1 & DSSE Attestations

`gke-model-admission-controller` uses **Dead Simple Signing Envelope (DSSE)** wrapping **in-toto Statement v1** (`https://in-toto.io/Statement/v1`) across all three cryptographic attestation types.

---

## 1. Why DSSE + in-toto Statement v1

DSSE signs the Pre-Authentication Encoding (`PAE(payloadType, payload)`) so the verifier validates the cryptographic signature **before** parsing untrusted JSON predicates, eliminating canonicalization ambiguities in the signed payload.

Every in-toto Statement v1 has the structure:

```json
{
  "_type": "https://in-toto.io/Statement/v1",
  "subject": [
    {
      "name": "gemma-2b-it",
      "digest": {
        "sha256": "<64-hex>"
      }
    }
  ],
  "predicateType": "<predicate-uri>",
  "predicate": { ... }
}
```

---

## 2. The Three Attestations Verified by the Controller

| Attestation | OCI Referrer `artifactType` | in-toto `predicateType` | `subject[0].digest.sha256` | Authorized Trust Domain |
|---|---|---|---|---|
| **1. OpenSSF Model Signing (OMS v1.0)** | `application/vnd.openssf.model-signing.signature.v1+json` | `https://model_signing/signature/v1.0` | `oms_root_digest` | `trustDomains.modelSigners` |
| **2. Behavioral Evaluation Evidence** | `application/vnd.devenes.evaluation.attestation.v1+json` | `https://devenes.github.io/ai-model-provenance-gate/attestation/evaluation-evidence/v1` | `bundle_id` (`bundle-identity/v1`) | `trustDomains.evaluators` |
| **3. Release Qualification** | `application/vnd.devenes.release-qualification.v1+json` | `https://devenes.github.io/ai-model-provenance-gate/attestation/release-qualification/v1` | `bundle_id` (`bundle-identity/v1`) | `trustDomains.releaseAuthorities` |

---

## 3. Native Evaluation Binding vs. Post-Hoc Endorsement

In `evaluation-evidence/v1` ([ADR-003](../adr/ADR-003-release-attestation.md)):

- **`NATIVE_EVALUATOR` (`attestation_kind: "EVALUATOR_NATIVE"`)**: Emitted directly by `llm-release-gate run --bundle-manifest ... --signing-key ...` at the moment the evaluation suite runs. The evaluator verifies `bundle-manifest.json`, binds `model_bundle_id` into `report.json`, and signs the DSSE attestation over both `bundle_id` and `report_digest`.
- **`POST_HOC_OPERATOR` (`attestation_kind: "OPERATOR_BINDING_ENDORSEMENT"`)**: Produced after the fact by `ai-model-provenance evidence bind-eval --producer-role operator-endorser`.
- When `ClusterModelTrustPolicy.spec.requirements.rejectPostHocBinding` is `true` (default in strict mode), the controller rejects `POST_HOC_OPERATOR` attestations with `PostHocEvaluationRejected` (`N-18`).
