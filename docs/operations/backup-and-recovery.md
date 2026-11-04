# Backup and Recovery Runbook

**GKE Model Admission Controller** is stateless at the pod level: all cluster state lives in Kubernetes CustomResources (`ClusterModelTrustPolicy` and `ModelArtifact`), while all cryptographic model evidence lives in Google Artifact Registry as immutable OCI v1.1 manifests.

---

## 1. Backing Up Custom Resources

Export your active `ClusterModelTrustPolicy` and all `ModelArtifact` specifications to version-controlled Git manifests or a backup snapshot:

```bash
mkdir -p backups/$(date +%Y%m%d)

kubectl get clustermodeltrustpolicy default -o yaml > backups/$(date +%Y%m%d)/clustermodeltrustpolicy-default.yaml
kubectl get modelartifacts --all-namespaces -o yaml > backups/$(date +%Y%m%d)/modelartifacts-all.yaml
```

---

## 2. Recovering from Accidental CRD or Policy Deletion

1. **If `ClusterModelTrustPolicy/default` is deleted**:
   - The webhook immediately enters fail-closed protection (`NO_TRUST_POLICY`), blocking new AI model deployments while leaving existing running Pods untouched.
   - Restore the policy:
     ```bash
     kubectl apply -f backups/latest/clustermodeltrustpolicy-default.yaml
     ```
   - Within seconds, `ClusterModelTrustPolicyReconciler` recomputes `status.policyDigest` and re-verifies all `ModelArtifact` resources.

2. **If a `ModelArtifact` is deleted**:
   - Re-apply the `ModelArtifact` YAML. Because the OCI bundle and its 4 evidence referrers remain immutable in Artifact Registry, the reconciler re-verifies the artifact from OCI descriptors in `< 500 ms`.
