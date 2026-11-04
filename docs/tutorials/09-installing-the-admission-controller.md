# Tutorial 09: Installing `cert-manager` and the Admission Controller via Helm

> **Series Navigation:** [08 Provisioning GKE with Terraform](08-provisioning-gke-with-terraform.md) ← **09** → [10 Configuring ClusterModelTrustPolicy](10-configuring-cluster-model-trust-policy.md)

---

## 1. What you will learn

- How to build and push the multi-stage distroless controller image (`gcr.io/distroless/static:nonroot`) to Google Artifact Registry and pin it by `@sha256:` digest.
- How to install `cert-manager` (`v1.17.1`) for automated webhook TLS certificate provisioning and CA bundle injection.
- How to install [`charts/model-admission-controller`](../../charts/model-admission-controller/) with 2 replicas, `PodDisruptionBudget`, and Workload Identity.

---

## 2. Prerequisites

- Completed [Tutorial 08](08-provisioning-gke-with-terraform.md) with `kubectl` connected to your GKE cluster.
- Docker and Helm `3.14+` installed locally.

---

## 3. Architecture context

The controller runs in `model-admission-system` with two replicas (`replicaCount: 2`), leader election enabled for CRD reconciliation, and both pods serving `/validate-workloads` HTTPS requests on port `9443`. `cert-manager` issues `Certificate/model-admission-controller-webhook-tls` and injects the CA bundle into `ValidatingWebhookConfiguration/model-admission-controller-webhook`.

---

## 4. Required input files

- [`Dockerfile`](../../Dockerfile)
- [`charts/model-admission-controller/Chart.yaml`](../../charts/model-admission-controller/Chart.yaml)
- [`charts/model-admission-controller/values.yaml`](../../charts/model-admission-controller/values.yaml)

---

## 5. Step-by-step instructions

1. Install `cert-manager` (`v1.17.1`) into the `cert-manager` namespace.
2. Build the `linux/amd64` controller container image from the workspace root (`..`) and push it to Artifact Registry.
3. Resolve the pushed image's `@sha256:` digest.
4. Install `charts/model-admission-controller` with `--set image.digest="${IMAGE_DIGEST}"` and the Workload Identity annotation.

---

## 6. Commands

```bash
export PROJECT_ID="your-gcp-project-id"
export REGION="us-west4"
export ARTIFACT_REGISTRY="${REGION}-docker.pkg.dev/${PROJECT_ID}/models"
export GCP_SA="gke-model-admission-ctrl@${PROJECT_ID}.iam.gserviceaccount.com"

# 1. Install cert-manager
helm repo add jetstack https://charts.jetstack.io --force-update
helm upgrade --install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --version v1.17.1 \
  --set crds.enabled=true \
  --wait

# 2. Build and push the distroless controller image
gcloud auth configure-docker "${REGION}-docker.pkg.dev" --quiet
docker build --platform linux/amd64 \
  -t "${ARTIFACT_REGISTRY}/gke-model-admission-controller:v0.1.0" \
  -f Dockerfile ..
docker push "${ARTIFACT_REGISTRY}/gke-model-admission-controller:v0.1.0"

# 3. Resolve the immutable image digest
export IMAGE_DIGEST=$(gcloud artifacts docker images describe \
  "${ARTIFACT_REGISTRY}/gke-model-admission-controller:v0.1.0" \
  --format='value(image_summary.digest)')
echo "Controller Image Digest: ${IMAGE_DIGEST}"

# 4. Install the controller Helm chart
helm upgrade --install model-admission-controller ./charts/model-admission-controller \
  --namespace model-admission-system \
  --create-namespace \
  --set image.repository="${ARTIFACT_REGISTRY}/gke-model-admission-controller" \
  --set image.digest="${IMAGE_DIGEST}" \
  --set serviceAccount.annotations."iam\.gke\.io/gcp-service-account"="${GCP_SA}" \
  --wait
```

---

## 7. Expected output

```text
Release "model-admission-controller" has been upgraded. Happy Helming!
NAME: model-admission-controller
NAMESPACE: model-admission-system
STATUS: deployed
REVISION: 1
```

---

## 8. How to verify success

Verify both replicas are `Running`, the `Certificate` is `Ready`, and the `PodDisruptionBudget` is active:

```bash
kubectl get pods,certificate,pdb -n model-admission-system
```

Expected output:

```text
NAME                                              READY   STATUS    RESTARTS   AGE
pod/model-admission-controller-5d4fd66b85-62zhv   1/1     Running   0          45s
pod/model-admission-controller-5d4fd66b85-8t9k7   1/1     Running   0          45s

NAME                                                            READY   SECRET                                   AGE
certificate.cert-manager.io/model-admission-controller-webhook-tls   True    model-admission-controller-webhook-tls   45s

NAME                                                      MIN AVAILABLE   MAX UNAVAILABLE   ALLOWED DISRUPTIONS   AGE
poddisruptionbudget.policy/model-admission-controller     1               N/A               1                     45s
```

---

## 9. Common errors

| Error | Cause | Fix |
|---|---|---|
| `ImagePullBackOff` on controller pod | Node ServiceAccount missing Artifact Registry reader permission or wrong `PROJECT_ID` | Verify `terraform apply` completed and `image.repository` points to `${REGION}-docker.pkg.dev/${PROJECT_ID}/models/...` |
| `Internal error occurred: failed calling webhook ... x509: certificate signed by unknown authority` | `cert-manager` cainjector has not finished injecting `caBundle` | Wait 10 seconds and check `kubectl get certificate -n model-admission-system` |

---

## 10. Security considerations

- **Distroless Non-Root Container**: The controller runs as `UID 65532` (`nonroot`) with `readOnlyRootFilesystem: true`, `allowPrivilegeEscalation: false`, and all Linux capabilities dropped (`drop: ["ALL"]`).
- **Digest-Pinned Controller Deployment**: Setting `image.digest` ensures the admission controller itself runs an immutable `@sha256:` image.

---

## 11. What comes next

Proceed to **[Tutorial 10: Configuring `ClusterModelTrustPolicy`](10-configuring-cluster-model-trust-policy.md)** to register your three domain-separated public keys and policy requirements in the cluster.
