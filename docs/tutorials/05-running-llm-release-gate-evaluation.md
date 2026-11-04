# Tutorial 05: Running Behavioral Evaluation with `llm-release-gate`

> **Series Navigation:** [04 Signing Model Artifacts](04-signing-model-artifacts.md) ← **05** → [06 Creating Release Qualification Attestation](06-creating-release-qualification-attestation.md)

---

## 1. What you will learn

- How `llm-release-gate run` executes a behavioral evaluation suite against a candidate model and binds the run directly to `bundle-manifest.json` (`NATIVE_CI_EVALUATOR`).
- How `llm-release-gate` signs the resulting `evaluation-evidence/v1` in-toto DSSE attestation (`evaluation.dsse.json`) using the `eval-signer` trust domain key.
- How `suite_digest`, `dataset_digest`, and `report_digest` are computed and pinned.

---

## 2. Prerequisites

- Completed [Tutorials 01–04](04-signing-model-artifacts.md) with `/tmp/model-work/bundle-manifest.json` and `/tmp/model-work/keys/eval-signer.key` ready.

---

## 3. Architecture context

In AI supply-chain security, a valid signature only proves *who* produced a model—not whether the model is safe or accurate. **`llm-release-gate`** enforces behavioral release gates (safety refusals, deterministic accuracy, latency, and schema conformance). When invoked with `--bundle-manifest` and `--signing-key`, `llm-release-gate` verifies the `bundle-identity/v1` manifest before running the suite and emits a DSSE attestation with:
- `attestation_kind: "TRUSTED_EVALUATOR_ATTESTATION"`
- `binding_mode: "NATIVE_CI_EVALUATOR"`
- `predicateType: "https://github.com/devenes/ai-model-provenance-gate/attestation/evaluation-evidence/v1"`

---

## 4. Required input files

- [`examples/fixtures/eval/suite.yaml`](../../examples/fixtures/eval/suite.yaml) (`suite.digest: sha256:2ef31d0e3121d5359f5e7d02d6ca627e2d3d0e259f06c3ecbf4360e88e0847ca`)
- [`examples/fixtures/eval/cases.jsonl`](../../examples/fixtures/eval/cases.jsonl) (`dataset.digest: sha256:c83d9a10096a9d2e4bfaaebb8b8e298f724a185c75c4887a81bfdcb4ecd5323a`)
- [`examples/fixtures/eval/candidate.yaml`](../../examples/fixtures/eval/candidate.yaml)
- `/tmp/model-work/bundle-manifest.json`
- `/tmp/model-work/keys/eval-signer.key`

---

## 5. Step-by-step instructions

1. Run `./bin/llm-release-gate run` passing `--suite`, `--candidate`, `--bundle-manifest`, `--signing-key`, `--signing-key-id eval-signer-1`, `--attestation-output`, and `--output`.
2. Inspect the generated `/tmp/model-work/report.json` (`llm-release-gate.report/v1`) and `/tmp/model-work/evaluation.dsse.json` (`evaluation-evidence/v1`).

---

## 6. Commands

```bash
./bin/llm-release-gate run \
  --suite ./examples/fixtures/eval/suite.yaml \
  --candidate ./examples/fixtures/eval/candidate.yaml \
  --bundle-manifest /tmp/model-work/bundle-manifest.json \
  --signing-key /tmp/model-work/keys/eval-signer.key \
  --signing-key-id eval-signer-1 \
  --attestation-output /tmp/model-work/evaluation.dsse.json \
  --output /tmp/model-work/report.json \
  --markdown /tmp/model-work/report.md
```

---

## 7. Expected output

```text
LLM Release Gate: PASS
Suite: release-qualification-smoke (2/2 cases passed, score 1.000)
JSON Report: /tmp/model-work/report.json
Markdown Report: /tmp/model-work/report.md
Signed Attestation: /tmp/model-work/evaluation.dsse.json (keyID=eval-signer-1, bundle=sha256:4ea48db6aa00c7908197674f0818f2c8a09821246ed021c8488ce84873cae97a)
```

---

## 8. How to verify success

Inspect the decoded `evaluation-evidence/v1` predicate inside `/tmp/model-work/evaluation.dsse.json`:

```bash
jq -r '.payload' /tmp/model-work/evaluation.dsse.json | base64 -d | jq '.predicate | {schema_version, attestation_kind, binding_mode, gate_status, model_bundle_id, suite_digest, dataset_digest}'
```

Expected output:

```json
{
  "schema_version": "evaluation-evidence/v1",
  "attestation_kind": "TRUSTED_EVALUATOR_ATTESTATION",
  "binding_mode": "NATIVE_CI_EVALUATOR",
  "gate_status": "PASS",
  "model_bundle_id": "sha256:4ea48db6aa00c7908197674f0818f2c8a09821246ed021c8488ce84873cae97a",
  "suite_digest": "sha256:2ef31d0e3121d5359f5e7d02d6ca627e2d3d0e259f06c3ecbf4360e88e0847ca",
  "dataset_digest": "sha256:c83d9a10096a9d2e4bfaaebb8b8e298f724a185c75c4887a81bfdcb4ecd5323a"
}
```

---

## 9. Common errors

| Error | Cause | Fix |
|---|---|---|
| `EVALUATION_INVALID` (`N-10`: `POST_HOC_OPERATOR`) | Attaching an evaluation report after the fact instead of binding `--bundle-manifest` at evaluation time | Always pass `--bundle-manifest` and `--signing-key` directly to `llm-release-gate run` |
| `EVALUATION_INVALID` (`N-12` / `N-13`) | `suite_digest` or `dataset_digest` differs from `ClusterModelTrustPolicy.spec.evaluationRequirements` | Ensure the policy's `expectedSuiteDigest` and `expectedDatasetDigest` match your approved benchmark suite |

---

## 10. Security considerations

- **Dual-Layer Evaluation Referrer (ADR-002)**: Both `evaluation.dsse.json` and `report.json` are packaged into the `application/vnd.devenes.evaluation.attestation.v1+json` OCI Referrer so the GKE controller can verify both the DSSE signature (`ReportAuthenticityVerified`) and the raw execution report (`ReportIntegrityVerified`, `ExecutedCases > 0`).

---

## 11. What comes next

Proceed to **[Tutorial 06: Creating the Release Qualification Attestation (`release-qualification/v1`)](06-creating-release-qualification-attestation.md)** to bind all upstream evidence, runtime image digest, and serving configuration digest into a single release attestation.
