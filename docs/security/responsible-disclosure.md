# Responsible Disclosure Policy

We take the security of **GKE Model Admission Controller**, `ai-model-provenance-gate`, and `llm-release-gate` seriously.

---

## Reporting a Vulnerability

**Do NOT open a public GitHub issue for suspected security vulnerabilities.**

Please report security vulnerabilities privately through one of the following channels:

1. **GitHub Private Vulnerability Reporting**:
   - Navigate to the **Security** tab of `https://github.com/devenes/gke-model-admission-controller` and click **"Report a vulnerability"**.
2. **Include in Your Report**:
   - Affected component (`internal/webhook`, `internal/controller`, `internal/policy`, `internal/registry`, `pkg/verify`, Helm chart, or Terraform configuration).
   - Step-by-step reproduction or proof-of-concept manifest.
   - Expected vs. actual admission or reconciliation behavior.

---

## Response Timeline

- **Initial acknowledgment**: Within 48 hours.
- **Triage & severity assessment**: Within 5 business days.
- **Coordinated fix & advisory release**: Within 30 days for critical/high issues.
