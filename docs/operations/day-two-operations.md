# Day-Two Operations Guide

This guide covers routine day-two operational tasks for platform engineers managing **GKE Model Admission Controller**.

---

## 1. Daily Health Check Commands

```bash
# 1. Verify controller pods and restarts
kubectl get pods -n model-admission-system -o wide

# 2. Verify webhook TLS certificate readiness
kubectl get certificate -n model-admission-system

# 3. Verify ClusterModelTrustPolicy readiness and policyDigest
kubectl get clustermodeltrustpolicy default

# 4. List all ModelArtifacts and their verification phase
kubectl get modelartifacts --all-namespaces
```

---

## 2. Inspecting Attestation Expiry (`status.validUntil`)

Both `evaluation-evidence/v1` and `release-qualification/v1` carry freshness windows (`maxAgeHours`, default `720` hours / 30 days). The controller records the earliest expiration timestamp in `ModelArtifact.status.validUntil`.

Check upcoming expirations across the cluster:

```bash
kubectl get modelartifacts --all-namespaces \
  -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name,PHASE:.status.phase,VALID_UNTIL:.status.validUntil
```

To refresh a `ModelArtifact` before `status.validUntil` expires:
1. Re-run `llm-release-gate run` against your current evaluation benchmark suite.
2. Re-run `ai-model-provenance gate evaluate` to issue a fresh `release-qualification.dsse.json`.
3. Push the updated referrers with `modelctl push` and update the `ModelArtifact` (or trigger re-reconciliation by annotating the `ModelArtifact`).

---

## 3. Viewing Structured Controller & Admission Audit Logs

Controller logs are emitted in structured JSON (`zap` production encoder) and automatically ingested into Google Cloud Logging:

```bash
# Tail live admission decisions and reconciliations
kubectl logs -n model-admission-system -l app.kubernetes.io/name=model-admission-controller -f --tail=50
```
