# Security Policy

## Supported Versions

| Version | Supported |
|---|---|
| `0.1.x` | Yes |

## Reporting a Vulnerability

Please **do not** report security vulnerabilities through public GitHub issues, discussions, or pull requests.

To report a suspected vulnerability in `gke-model-admission-controller`, [`ai-model-provenance-gate`](https://github.com/devenes/ai-model-provenance-gate), or [`llm-release-gate`](https://github.com/devenes/llm-release-gate):

1. Use **GitHub Private Vulnerability Reporting** (`Security` -> `Report a vulnerability`) on the repository:
   https://github.com/devenes/gke-model-admission-controller/security/advisories/new
2. Include:
   - Affected component (`internal/admission`, `internal/verification`, `internal/registry`, `internal/policy`, CRD schema, Helm chart, or Terraform configuration).
   - Step-by-step reproduction instructions or a minimal Go test / Kubernetes YAML manifest.
   - Expected fail-closed behavior vs. actual observed behavior.
   - Potential supply-chain or cluster impact.

## Scope

In-scope security issues include:

- **Admission Bypass**: Any pathway allowing an unverified, expired, or tampered model workload (`Pod`, `Deployment`, `StatefulSet`, `DaemonSet`, `Job`, `CronJob`) to be admitted in a namespace labeled `trust.gke.dev/enforce: "true"`.
- **Exemption Escalation**: Any mechanism allowing an unauthorized workload creator to self-assign an admission exemption via Pod labels, annotations, or `spec.serviceAccountName`.
- **Cross-Domain Key Confusion**: Accepting a signature from one trust domain (`modelSigners`, `evaluators`, `releaseAuthorities`) for an artifact belonging to another trust domain.
- **Digest Conflation or Substitution**: Bypassing the cryptographic binding between the OCI Subject Manifest digest, `bundle-identity/v1` `bundle_id`, OMS v1.0 root/resource digests, CycloneDX 1.7 BOM hash, `evaluation-evidence/v1`, or `release-qualification/v1`.
- **Resource Exhaustion / DoS**: Bypassing `MaxMetadataBlobBytes` (`16 MiB`) to force the controller to download multi-gigabyte model weight blobs.

For full details on what the system guarantees and its architectural boundaries, see:

- [Security Guarantees](docs/SECURITY_GUARANTEES.md)
- [Security Limitations](docs/LIMITATIONS.md)
- [Threat Model (TM-01 .. TM-12)](docs/THREAT_MODEL.md)
