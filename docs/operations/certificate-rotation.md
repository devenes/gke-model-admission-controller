# Webhook TLS Certificate Rotation Runbook

The `ValidatingAdmissionWebhook` serves HTTPS on port `9443` using a TLS certificate managed by **`cert-manager`**.

---

## 1. How TLS Certificates Are Provisioned and Rotated

In [`charts/model-admission-controller/templates/certificate.yaml`](../../charts/model-admission-controller/templates/certificate.yaml):

1. `Issuer/model-admission-controller-selfsigned` issues `Certificate/model-admission-controller-webhook-tls`.
2. `cert-manager` stores the TLS keypair in `Secret/model-admission-controller-webhook-tls` (`namespace: model-admission-system`), mounted into the controller pod at `/tmp/k8s-webhook-server/serving-certs`.
3. `ValidatingWebhookConfiguration/model-admission-controller-webhook` carries the annotation:
   ```yaml
   cert-manager.io/inject-ca-from: model-admission-system/model-admission-controller-webhook-tls
   ```
   `cert-manager`'s `cainjector` automatically updates `.webhooks[0].clientConfig.caBundle` whenever the certificate rotates, and `controller-runtime`'s `certwatcher` hot-reloads `/tmp/k8s-webhook-server/serving-certs/tls.crt` and `tls.key` in memory without restarting the controller pods.

---

## 2. Inspecting Certificate Status and Expiry

```bash
kubectl get certificate -n model-admission-system -o wide
kubectl describe certificate model-admission-controller-webhook-tls -n model-admission-system
```

---

## 3. Forcing Manual Certificate Renewal

If you need to rotate the webhook TLS certificate immediately:

```bash
# Delete the generated TLS Secret; cert-manager will immediately re-issue it
kubectl delete secret model-admission-controller-webhook-tls -n model-admission-system

# Wait for cert-manager to recreate the Secret and mark the Certificate Ready
kubectl wait --for=condition=Ready certificate/model-admission-controller-webhook-tls \
  -n model-admission-system --timeout=60s
```
