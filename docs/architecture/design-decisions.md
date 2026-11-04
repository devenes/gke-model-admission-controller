# Design Decisions & ADR Index (`design-decisions.md`)

This document summarizes the key engineering decisions in `gke-model-admission-controller` and links to the formal Architecture Decision Records (ADRs).

---

## 1. Summary of Key Decisions

| Decision | Chosen Approach | Rejected Alternative | Why |
|---|---|---|---|
| **ADR-001: Verification Engine Invocation** | Import `github.com/devenes/ai-model-provenance-gate/pkg/verify` as an in-memory Go library. | Execute `ai-model-provenance` or `llm-release-gate` via `os/exec` subprocesses. | Subprocess execution requires temporary disk I/O inside a `readOnlyRootFilesystem` container, adds process fork overhead, and widens the container attack surface. |
| **ADR-002: Model & Evidence Distribution** | OCI Distribution v1.1 Subject Manifest + 4 Referrers (`/v2/<repo>/referrers/<digest>`) with descriptor-only weight verification (`<= 16 MiB`). | Custom HTTP metadata sidecar or downloading full model weight blobs during reconciliation. | Uses standard Google Artifact Registry APIs with Workload Identity and keeps controller memory bounded (`<256Mi`) regardless of whether model weights are `100 MB` or `400 GB`. |
| **ADR-003: Evaluation Binding & Signer Domains** | Native evaluation-time DSSE signing (`llm-release-gate run --bundle-manifest --signing-key`) + 3 isolated trust domains (`modelSigners`, `evaluators`, `releaseAuthorities`). | Single shared signing key or post-hoc operator endorsement (`POST_HOC_OPERATOR`). | Prevents cross-domain key confusion (`N-17`) and blocks operators from attaching unverified or replayed evaluation reports after the fact (`N-18`). |
| **Reconciliation vs. In-Webhook Verification** | Reconcile OCI evidence into `ModelArtifact.status` asynchronously; keep `ValidatingAdmissionWebhook` 100% network-I/O-free. | Query Artifact Registry synchronously inside `ValidatingAdmissionWebhook`. | Guarantees `<50ms` webhook latency, prevents thundering-herd registry outages during HPA scale-out, and survives transient registry failures while TTL is valid. |
| **Exemption Authentication** | Require Kubernetes API-server-authenticated `req.UserInfo.Username` + `exemptPodLabels`. | Trust `pod.spec.serviceAccountName` or Pod labels alone. | Any user with `create pods` permission can write arbitrary labels or `spec.serviceAccountName` in Pod YAML; only `req.UserInfo.Username` is authenticated by the API server (`P-10`). |

---

## 2. Full Architecture Decision Records

- **[ADR-001: Public In-Memory Verifier API (`pkg/verify`)](../adr/ADR-001-verifier-api.md)**
- **[ADR-002: OCI v1.1 Model Bundle & Referrer Mapping](../adr/ADR-002-oci-model-artifact.md)**
- **[ADR-003: Native `llm-release-gate` DSSE Evaluation Attestation](../adr/ADR-003-release-attestation.md)**
