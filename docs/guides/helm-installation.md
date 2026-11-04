# Helm Installation Guide

The Kubernetes operator, CRDs, RBAC, TLS certificates, `ValidatingWebhookConfiguration`, and `PodDisruptionBudget` are packaged in [`charts/model-admission-controller/`](../../charts/model-admission-controller/).

---

## 1. Chart Structure

```text
charts/model-admission-controller/
├── Chart.yaml
├── values.yaml
├── crds/
│   ├── trust.gke.dev_clustermodeltrustpolicies.yaml
│   └── trust.gke.dev_modelartifacts.yaml
└── templates/
    ├── _helpers.tpl
    ├── certificate.yaml           # cert-manager Issuer + Certificate
    ├── deployment.yaml            # 2-replica hardened Deployment
    ├── namespace.yaml             # model-admission-system + ai-serving namespaces
    ├── pdb.yaml                   # PodDisruptionBudget (minAvailable: 1)
    ├── rbac.yaml                  # ClusterRole + ClusterRoleBinding
    ├── service.yaml               # Webhook HTTPS (443->9443) + Metrics HTTP (8080)
    ├── serviceaccount.yaml        # Workload Identity annotated ServiceAccount
    └── validatingwebhook.yaml     # Fail-closed ValidatingWebhookConfiguration
```

---

## 2. Configuration Values (`values.yaml`)

| Key | Default | Description |
|---|---|---|
| `replicaCount` | `2` | Number of controller replicas (paired with anti-affinity and PDB) |
| `image.repository` | `us-west4-docker.pkg.dev/PROJECT_ID/models/gke-model-admission-controller` | Controller container image repository |
| `image.tag` | `v0.1.0` | Fallback tag if `image.digest` is empty |
| `image.digest` | `""` | **Recommended**: Immutable `sha256:...` digest (`repository@sha256:...`) |
| `serviceAccount.name` | `model-admission-controller` | Kubernetes ServiceAccount name |
| `serviceAccount.annotations` | `{}` | Set `iam.gke.io/gcp-service-account` for GKE Workload Identity |
| `webhook.port` | `9443` | Container port for the TLS `ValidatingAdmissionWebhook` |
| `webhook.failurePolicy` | `Fail` | Fail-closed admission policy (`Fail`) |
| `webhook.timeoutSeconds` | `10` | Webhook timeout in seconds |
| `certManager.enabled` | `true` | Provision self-signed `Issuer` and `Certificate` via `cert-manager` |
| `protectedNamespaces.createDefault` | `true` | Create the `ai-serving` namespace labeled with `model-admission.gke.io/enforce: "true"` |
| `podDisruptionBudget.enabled` | `true` | Create `PodDisruptionBudget` with `minAvailable: 1` |

---

## 3. Installing or Upgrading the Chart

```bash
helm upgrade --install model-admission-controller ./charts/model-admission-controller \
  --namespace model-admission-system \
  --create-namespace \
  --set image.repository="${ARTIFACT_REGISTRY}/gke-model-admission-controller" \
  --set image.digest="${IMAGE_DIGEST}" \
  --set serviceAccount.annotations."iam\.gke\.io/gcp-service-account"="gke-model-admission-ctrl@${PROJECT_ID}.iam.gserviceaccount.com" \
  --wait
```

---

## 4. Validating Rendered Manifests Offline

Before installing onto a cluster, lint and render the templates locally:

```bash
helm lint ./charts/model-admission-controller
helm template model-admission-controller ./charts/model-admission-controller > /tmp/rendered.yaml
```
