# Tutorial 10: Configuring `ClusterModelTrustPolicy`

> **Series Navigation:** [09 Installing the Admission Controller](09-installing-the-admission-controller.md) ← **10** → [11 Registering a ModelArtifact](11-registering-a-modelartifact.md)

---

## 1. What you will learn

- How to configure the cluster-scoped `ClusterModelTrustPolicy/default` CustomResource with your three isolated trust domain public keys (`modelSigners`, `evaluatorSigners`, `releaseSigners`).
- How `ClusterModelTrustPolicyReconciler` validates each ECDSA P-256 public key and computes the deterministic RFC 8785 JCS `status.policyDigest`.

---

## 2. Prerequisites

- Completed [Tutorials 01–09](09-installing-the-admission-controller.md) with `/tmp/model-work/keys/*.pub` generated and the controller running in `model-admission-system`.

---

## 3. Architecture context

`ClusterModelTrustPolicy/default` is the single source of policy truth for the entire cluster. Whenever `ClusterModelTrustPolicy/default` changes, its `metadata.generation` increments and `status.policyDigest` is recomputed using RFC 8785 JCS over the spec and resolved PKIX key fingerprints. Any `ModelArtifact` whose `status.evaluatedPolicyDigest` does not match `ClusterModelTrustPolicy/default.status.policyDigest` is blocked from deployment until re-reconciled.

---

## 4. Required input files

- `/tmp/model-work/keys/model-signer.pub`
- `/tmp/model-work/keys/eval-signer.pub`
- `/tmp/model-work/keys/release-signer.pub`
- [`examples/clustermodeltrustpolicy-default.yaml`](../../examples/clustermodeltrustpolicy-default.yaml) (reference template)

---

## 5. Step-by-step instructions

1. Generate `/tmp/model-work/clustermodeltrustpolicy.yaml` embedding the PEM public keys from `/tmp/model-work/keys/`.
2. Apply `/tmp/model-work/clustermodeltrustpolicy.yaml` to the GKE cluster.
3. Verify that `ClusterModelTrustPolicy/default` transitions to `Ready=True` and populates `status.policyDigest`.

---

## 6. Commands

```bash
export PROJECT_ID="${PROJECT_ID:-your-gcp-project-id}"
export MODEL_PUB=$(sed 's/^/        /' /tmp/model-work/keys/model-signer.pub)
export EVAL_PUB=$(sed 's/^/        /' /tmp/model-work/keys/eval-signer.pub)
export RELEASE_PUB=$(sed 's/^/        /' /tmp/model-work/keys/release-signer.pub)

cat <<EOF > /tmp/model-work/clustermodeltrustpolicy.yaml
apiVersion: trust.gke.dev/v1alpha1
kind: ClusterModelTrustPolicy
metadata:
  name: default
spec:
  enforcementMode: Enforce
  allowedModelFormats:
    - safetensors
  rejectUnsafePickle: true
  rejectRemoteCodeHooks: true
  modelSigners:
    - id: model-signer-1
      name: Production Model Signer
      publicKeyPEM: |
${MODEL_PUB}
  evaluatorSigners:
    - id: eval-signer-1
      name: Trusted Evaluation Runner
      role: trusted-evaluator
      publicKeyPEM: |
${EVAL_PUB}
  releaseSigners:
    - id: release-signer-1
      name: Release Qualification Authority
      publicKeyPEM: |
${RELEASE_PUB}
  bomRequirements:
    required: true
    minSpecVersion: "1.7"
    requireModelCard: true
  evaluationRequirements:
    required: true
    requireTrustedEvaluator: true
    rejectPostHocBinding: true
    maxAgeHours: 720
    expectedSuiteDigest: "sha256:2ef31d0e3121d5359f5e7d02d6ca627e2d3d0e259f06c3ecbf4360e88e0847ca"
    expectedDatasetDigest: "sha256:c83d9a10096a9d2e4bfaaebb8b8e298f724a185c75c4887a81bfdcb4ecd5323a"
  releaseRequirements:
    required: true
    maxAgeHours: 720
    allowedRuntimeImages:
      - "us-west4-docker.pkg.dev/${PROJECT_ID}/models/vllm-runtime@sha256:*"
    requireRuntimeDigestPinning: true
    requireServingConfigDigestMatch: true
  exemptions:
    allowedServiceAccounts:
      - "system:serviceaccount:ai-serving:breakglass-deployer"
EOF

kubectl apply -f /tmp/model-work/clustermodeltrustpolicy.yaml
kubectl get clustermodeltrustpolicy default
```

---

## 7. Expected output

```text
clustermodeltrustpolicy.trust.gke.dev/default configured
NAME      MODE      POLICYDIGEST                                                              AGE
default   Enforce   sha256:322cb55cf42416d9e9b1793f9dbcd1839c7ccdf5be76898119b1e711f0dbefaf   10s
```

---

## 8. How to verify success

Inspect the full `status` block of `ClusterModelTrustPolicy/default`:

```bash
kubectl get clustermodeltrustpolicy default -o jsonpath='{.status}' | jq .
```

Expected: `conditions[0].type == "Ready"`, `conditions[0].status == "True"`, `conditions[0].reason == "PolicyValid"`, and `policyDigest` starting with `sha256:`.

---

## 9. Common errors

| Error | Cause | Fix |
|---|---|---|
| `Reason: InvalidPublicKey` | Malformed PEM block or private key accidentally pasted instead of `PUBLIC KEY` | Ensure `-----BEGIN PUBLIC KEY-----` PKIX PEM format is used |
| `NO_TRUST_POLICY` at admission | Policy named something other than `default` | Ensure `metadata.name: default` |

---

## 10. Security considerations

- **Automatic Cluster-Wide Re-Evaluation**: Updating any field or key in `ClusterModelTrustPolicy/default` immediately triggers re-reconciliation of every `ModelArtifact` in the cluster (`enqueueAllModelArtifacts`).

---

## 11. What comes next

Proceed to **[Tutorial 11: Registering and Verifying a `ModelArtifact` CustomResource](11-registering-a-modelartifact.md)**.
