# Security Overview

**GKE Model Admission Controller** enforces a fail-closed, cryptographically bound AI model supply-chain gate on Google Kubernetes Engine (GKE).

---

## 1. Security Documentation Index

- **[Threat Model (`threat-model.md`)](threat-model.md)**: STRIDE threat analysis, trust boundaries, attacker capabilities, and the 28-scenario negative test matrix (`N-01` through `N-28`).
- **[Security Guarantees (`security-guarantees.md`)](security-guarantees.md)**: Exact cryptographic and Kubernetes admission invariants enforced by the controller and verified on GKE.
- **[Security Limitations (`security-limitations.md`)](security-limitations.md)**: Explicit non-goals, out-of-scope runtime attack vectors, and defense-in-depth recommendations.
- **[Responsible Disclosure (`responsible-disclosure.md`)](responsible-disclosure.md)**: How to report security vulnerabilities privately via GitHub Security Advisories.
