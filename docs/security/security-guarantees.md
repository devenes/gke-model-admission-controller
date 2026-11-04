# Security Guarantees

For the complete list of cryptographic, OCI, and Kubernetes admission invariants verified in code and on GKE, see [Security Guarantees (`docs/SECURITY_GUARANTEES.md`)](../SECURITY_GUARANTEES.md).

## Summary of Core Guarantees

1. **Deterministic Content-Addressed Identity (`bundle-identity/v1`)**: Computed via RFC 8785 JCS over every file path, byte size, SHA-256 digest, and role classification.
2. **Zero-Weight-Download Verification**: Recomputes `bundle-identity/v1` directly from OCI manifest `layers[]` descriptors without pulling multi-gigabyte weight blobs.
3. **Three Isolated Cryptographic Trust Domains**: `modelSigners`, `evaluatorSigners`, and `releaseSigners` are strictly isolated; cross-domain key reuse is rejected as `StatusUntrusted`.
4. **Cross-Evidence Digest Pinning**: `release-qualification/v1` cryptographically binds `manifest_digest`, `bom.digest`, `oms_verification.signature_digest`, `evaluation.report_digest`, and `evaluation.attestation_digest`.
5. **Fail-Closed Admission Enforcement**: Intercepts `Pod`, `Deployment`, `StatefulSet`, `DaemonSet`, `Job`, and `CronJob` with `failurePolicy: Fail`, default-deny heuristics in protected namespaces, and authenticated `UserInfo.Username` exemption checks.
