# Security Limitations & Non-Goals

For the complete architectural and operational boundaries of this project, see [Limitations & Future Work (`docs/LIMITATIONS.md`)](../LIMITATIONS.md).

## Key Security Boundaries

1. **OCI Descriptor Trust vs. Node Runtime Pull Verification**:
   - The controller verifies OCI manifest and layer descriptors from Artifact Registry without downloading multi-gigabyte `.safetensors` blobs into the controller pod.
   - To guarantee that the inference container on the worker node verifies every byte of the `.safetensors` blob on download, pair this admission controller with an immutable digest-pinned CSI/init-container fetcher that verifies layer SHA-256 digests on read.
2. **Admission-Time Gate vs. Post-Start Container Mutation**:
   - Kubernetes `ValidatingAdmissionWebhooks` run on `CREATE` and `UPDATE` API operations. Pair this controller with restricted `kubectl exec` RBAC, read-only root filesystems, and GKE Workload Vulnerability Scanning for runtime defense-in-depth.
3. **Static PEM Public Keys in v0.1.0**:
   - `ClusterModelTrustPolicy` currently verifies ECDSA P-256 PEM keys (`publicKeyPEM`). Fulcio keyless OIDC + Rekor transparency log verification is planned on the roadmap.
