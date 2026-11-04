# Audit Mode & Non-Self-Assignable Exemptions

When rolling out AI model admission governance across existing GKE clusters, platform teams often need a staged rollout (`Audit` -> `Enforce`) and a tightly controlled break-glass mechanism for incident response.

---

## 1. `Audit` vs. `Enforce` Mode

`ClusterModelTrustPolicy.spec.enforcementMode` controls how the `ValidatingAdmissionWebhook` responds when a workload violates a trust requirement:

| Mode | Webhook Admission Decision | Kubernetes Warning Header | Prometheus Metrics (`model_admission_decisions_total`) |
|---|---|---|---|
| **`Enforce`** *(Default)* | **DENIED** (`Allowed: false`) | None (request rejected with `[REASON] message`) | Increments `decision="deny", mode="Enforce", reason="<REASON>"` |
| **`Audit`** | **ALLOWED** (`Allowed: true`) | Returns HTTP Warning header: `[AUDIT-VIOLATION: <REASON>] <message>` | Increments `decision="audit_warn", mode="Audit", reason="<REASON>"` |

### Switching to `Audit` Mode

```bash
kubectl patch clustermodeltrustpolicy default \
  --type=merge \
  -p '{"spec":{"enforcementMode":"Audit"}}'
```

When a non-compliant workload is applied in `Audit` mode, `kubectl apply` succeeds and prints the exact violation warning to the user's terminal:

```text
Warning: [AUDIT-VIOLATION: RUNTIME_IMAGE_NOT_DIGEST_PINNED] container "vllm" image "us-west4-docker.pkg.dev/PROJECT_ID/models/vllm-runtime:latest" must be pinned by @sha256: digest
deployment.apps/unpinned-vllm created
```

### Switching Back to `Enforce` Mode

```bash
kubectl patch clustermodeltrustpolicy default \
  --type=merge \
  -p '{"spec":{"enforcementMode":"Enforce"}}'
```

---

## 2. Non-Self-Assignable Break-Glass Exemptions (`P-10` / `N-27`)

A common anti-pattern in Kubernetes admission webhooks is allowing any user with `pods/create` or `deployments/create` permission to bypass the webhook simply by adding an annotation like `exempt: "true"` to their YAML manifest.

**GKE Model Admission Controller closes this loophole by binding the exemption annotation to the authenticated Kubernetes API caller (`AdmissionRequest.UserInfo.Username`).**

### How Authenticated Exemption Enforcement Works (`internal/webhook/admission.go`)

When a workload carries `trust.gke.dev/exempt: "true"`:

1. The webhook inspects `req.UserInfo.Username` (populated by the Kubernetes API server's authentication layer, which cannot be forged in the YAML payload).
2. It compares `req.UserInfo.Username` against `ClusterModelTrustPolicy.spec.exemptions.allowedServiceAccounts`.
3. **If `req.UserInfo.Username` is NOT in `allowedServiceAccounts`**:
   - Even in `Audit` mode, an unauthorized exemption attempt is **DENIED immediately** with `UNAUTHORIZED_EXEMPTION`:
     ```text
     [UNAUTHORIZED_EXEMPTION] caller "system:serviceaccount:ai-serving:unauthorized-deployer" is not authorized to use trust.gke.dev/exempt in ClusterModelTrustPolicy
     ```
4. **If `req.UserInfo.Username` IS in `allowedServiceAccounts`**:
   - The webhook allows the workload, emits an audit warning (`exemption granted to caller ...`), and increments `model_admission_decisions_total{decision="allow", reason="EXEMPT_AUTHORIZED"}`.

> [!IMPORTANT]
> **Direct Pod Creation vs. Controller-Managed Workloads (`Deployment`)**
> When a `Deployment` is created, the `AdmissionRequest` for the `Deployment` resource carries the caller's identity (e.g. `system:serviceaccount:ai-serving:breakglass-deployer`), whereas the downstream `Pod` creation request is issued by `system:serviceaccount:kube-system:replicaset-controller`. Therefore, break-glass exemptions are either applied to direct `Pod` creation by the authorized ServiceAccount or require explicit authorization of the workload controller path.

### Configuring an Authorized Break-Glass ServiceAccount

```yaml
apiVersion: trust.gke.dev/v1alpha1
kind: ClusterModelTrustPolicy
metadata:
  name: default
spec:
  exemptions:
    allowedServiceAccounts:
      - "system:serviceaccount:ai-serving:breakglass-deployer"
```
