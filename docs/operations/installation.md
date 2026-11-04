# Production Installation Runbook

This runbook provides the production installation checklist for deploying **GKE Model Admission Controller** onto a GKE cluster.

---

## 1. Pre-Installation Checklist

- [ ] GKE cluster running Kubernetes `1.29+` with **Workload Identity** enabled (`PROJECT_ID.svc.id.goog`).
- [ ] Control-plane-to-node firewall rule allows TCP `9443` from the GKE control plane CIDR (`172.16.0.0/28` or cluster master ipv4 cidr block) to worker nodes.
- [ ] `cert-manager` (`v1.17.1+`) installed and `cert-manager-webhook` pod Ready.
- [ ] Google Cloud Service Account (`gke-model-admission-ctrl@PROJECT_ID.iam.gserviceaccount.com`) granted:
  - `roles/artifactregistry.reader` on the target Artifact Registry repository.
  - `roles/iam.workloadIdentityUser` for `PROJECT_ID.svc.id.goog[model-admission-system/model-admission-controller]`.
- [ ] Controller container image pushed to Artifact Registry and pinned by `@sha256:` digest.

---

## 2. Step-by-Step Installation

### Step 1: Install CRDs and Controller Chart

```bash
helm upgrade --install model-admission-controller ./charts/model-admission-controller \
  --namespace model-admission-system \
  --create-namespace \
  --set image.repository="${ARTIFACT_REGISTRY}/gke-model-admission-controller" \
  --set image.digest="${IMAGE_DIGEST}" \
  --set serviceAccount.annotations."iam\.gke\.io/gcp-service-account"="gke-model-admission-ctrl@${PROJECT_ID}.iam.gserviceaccount.com" \
  --wait
```

### Step 2: Verify TLS Certificate and Webhook CA Injection

```bash
kubectl get certificate -n model-admission-system
kubectl get validatingwebhookconfiguration model-admission-controller-webhook \
  -o jsonpath='{.webhooks[0].clientConfig.caBundle}' | wc -c
```

Ensure `Certificate/model-admission-controller-webhook-tls` shows `READY = True` and the injected `caBundle` byte count is `> 1000`.

### Step 3: Apply Initial `ClusterModelTrustPolicy` in `Audit` Mode (Optional Staged Rollout)

For existing clusters with running workloads, apply `ClusterModelTrustPolicy/default` with `enforcementMode: Audit` first, register and verify all existing `ModelArtifact` resources, and then switch `enforcementMode: Enforce`.

```bash
kubectl apply -f examples/clustermodeltrustpolicy.yaml
kubectl get clustermodeltrustpolicy default
```

### Step 4: Label Protected Namespaces

```bash
kubectl label namespace ai-serving model-admission.gke.io/enforce=true --overwrite
```
