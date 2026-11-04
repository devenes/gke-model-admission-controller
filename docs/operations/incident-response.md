# Security Incident Response Runbook

This runbook covers emergency operational procedures when responding to a suspected AI model supply-chain compromise, a bad model release, or an urgent production outage.

---

## Scenario 1: Compromised or Unsafe Model Bundle Identified

Suppose a deployed model (`gemma-2b-it` or a fine-tuned LoRA adapter) is discovered to have data poisoning, prompt-injection vulnerabilities, or a compromised signer key.

### Immediate Containment Steps

1. **Block New Admissions Immediately**:
   Delete or mark the offending `ModelArtifact`, or revoke its signer key in `ClusterModelTrustPolicy/default`:
   ```bash
   kubectl delete modelartifact <compromised-model> -n ai-serving
   ```
   Once deleted, any attempt to scale up or deploy workloads referencing `<compromised-model>` is immediately rejected (`MODEL_ARTIFACT_NOT_FOUND`).

2. **Evict Running Inference Workloads Using the Model**:
   Locate and scale down or delete all workloads annotated with the compromised `ModelArtifact`:
   ```bash
   kubectl get deployments,statefulsets,daemonsets,jobs,pods -n ai-serving \
     -o json | jq -r '.items[] | select(.metadata.annotations["trust.gke.dev/model-artifact"]=="<compromised-model>") | "\(.kind)/\(.metadata.name)"'
   ```

---

## Scenario 2: Break-Glass Emergency Deployment (`trust.gke.dev/exempt: "true"`)

If an urgent incident requires deploying a hotfix pod before full evaluation suites complete:

1. Ensure the incident responder's Kubernetes ServiceAccount is listed in `ClusterModelTrustPolicy.spec.exemptions.allowedServiceAccounts`:
   ```yaml
   exemptions:
     allowedServiceAccounts:
       - "system:serviceaccount:ai-serving:breakglass-deployer"
   ```
2. Annotate the emergency workload with `trust.gke.dev/exempt: "true"` and apply it **as that authorized ServiceAccount**:
   ```bash
   kubectl apply --as=system:serviceaccount:ai-serving:breakglass-deployer -f emergency-pod.yaml
   ```
3. Every exempted admission emits a Kubernetes warning and increments `model_admission_decisions_total{decision="allow", reason="EXEMPT_AUTHORIZED"}` for post-incident audit review.
4. Remove the break-glass ServiceAccount from `allowedServiceAccounts` as soon as the incident closes.
