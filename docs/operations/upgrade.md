# Zero-Downtime Upgrade Runbook

Because the `ValidatingWebhookConfiguration` enforces `failurePolicy: Fail`, upgrading the controller must maintain at least one ready webhook endpoint at all times.

---

## 1. High-Availability Architecture Guarantees

The Helm chart configures three layers of availability protection:
1. **`replicaCount: 2`** in [`deployment.yaml`](../../charts/model-admission-controller/templates/deployment.yaml).
2. **`PodDisruptionBudget`** (`minAvailable: 1`) in [`pdb.yaml`](../../charts/model-admission-controller/templates/pdb.yaml).
3. **Pod Anti-Affinity** (`preferredDuringSchedulingIgnoredDuringExecution`) spreading the two replicas across separate GKE worker nodes.
4. **Leader Election (`--leader-elect=true`)** for the asynchronous `ModelArtifact` and `ClusterModelTrustPolicy` reconcilers while **both** replicas actively serve synchronous `/validate-workloads` HTTPS admission requests.

---

## 2. Upgrading the Controller Image

### Step 1: Build and Push the New Image Digest

```bash
docker build --platform linux/amd64 \
  -t "${ARTIFACT_REGISTRY}/gke-model-admission-controller:v0.1.1" \
  -f Dockerfile ..

docker push "${ARTIFACT_REGISTRY}/gke-model-admission-controller:v0.1.1"

export NEW_IMAGE_DIGEST=$(gcloud artifacts docker images describe \
  "${ARTIFACT_REGISTRY}/gke-model-admission-controller:v0.1.1" \
  --format='value(image_summary.digest)')
```

### Step 2: Upgrade CRDs (If Schema Changed)

Helm does not automatically upgrade CRDs inside `crds/` on `helm upgrade`. Apply `config/crd/bases/` explicitly before running `helm upgrade`:

```bash
kubectl apply -f config/crd/bases/
```

### Step 3: Execute Rolling Helm Upgrade

```bash
helm upgrade model-admission-controller ./charts/model-admission-controller \
  --namespace model-admission-system \
  --reuse-values \
  --set image.digest="${NEW_IMAGE_DIGEST}" \
  --wait
```

### Step 4: Verify Rollout and Running Image Digests

```bash
kubectl rollout status deployment/model-admission-controller -n model-admission-system
kubectl get pods -n model-admission-system \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.containerStatuses[0].imageID}{"\n"}{end}'
```

---

## 3. Rollback Procedure

If a regression is detected after an upgrade, roll back immediately to the previous Helm revision:

```bash
helm history model-admission-controller -n model-admission-system
helm rollback model-admission-controller 0 -n model-admission-system --wait
```
