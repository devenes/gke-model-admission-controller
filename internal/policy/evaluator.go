// Package policy resolves ClusterModelTrustPolicy trust domains, public key material,
// and deterministic RFC 8785 (JCS) policy digests.
package policy

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/devenes/ai-model-provenance-gate/pkg/verify"
	"github.com/devenes/gke-model-admission-controller/api/v1alpha1"
)

// SecretFetcher retrieves raw key bytes from a Kubernetes Secret.
type SecretFetcher func(ctx context.Context, namespace, name, key string) ([]byte, error)

// ResolvedPolicy holds the resolved cryptographic trust domains, effective requirements,
// and deterministic RFC 8785 JCS policyDigest of a ClusterModelTrustPolicy.
type ResolvedPolicy struct {
	Name                string
	Generation          int64
	EnforcementMode     string
	PolicyDigest        string
	AllowedRegistries   []string
	VerificationTTL     time.Duration
	MaxEvidenceAge      time.Duration
	Requirements        v1alpha1.PolicyRequirementsSpec
	Exemptions          v1alpha1.PolicyExemptionsSpec
	ModelSigners        []verify.TrustedSignerRule
	Evaluators          []verify.TrustedSignerRule
	ReleaseAuthorities  []verify.TrustedSignerRule
	AllDomainCandidates []verify.TrustedSignerRule
	ModelSignerFPs      []string
	EvaluatorFPs        []string
	ReleaseAuthorityFPs []string
}

type canonicalSignerEntry struct {
	ID                       string `json:"id"`
	KeyID                    string `json:"key_id"`
	KeyFingerprint           string `json:"key_fingerprint"`
	Algorithm                string `json:"algorithm"`
	AllowOperatorEndorsement bool   `json:"allow_operator_endorsement"`
}

type canonicalPolicyDocument struct {
	SchemaVersion          string                          `json:"schema_version"`
	EnforcementMode        string                          `json:"enforcement_mode"`
	AllowedRegistries      []string                        `json:"allowed_registries"`
	VerificationTTLSeconds int64                           `json:"verification_ttl_seconds"`
	MaxEvidenceAgeHours    int                             `json:"max_evidence_age_hours"`
	ModelSigners           []canonicalSignerEntry          `json:"model_signers"`
	Evaluators             []canonicalSignerEntry          `json:"evaluators"`
	ReleaseAuthorities     []canonicalSignerEntry          `json:"release_authorities"`
	Requirements           v1alpha1.PolicyRequirementsSpec `json:"requirements"`
	ExemptServiceAccounts  []string                        `json:"exempt_service_accounts,omitempty"`
	ExemptPodLabels        map[string]string               `json:"exempt_pod_labels,omitempty"`
}

// Resolve validates a ClusterModelTrustPolicy, resolves all inline and Secret-backed public keys,
// enforces key fingerprint pinning, and computes the deterministic RFC 8785 (JCS) policyDigest.
func Resolve(ctx context.Context, pol *v1alpha1.ClusterModelTrustPolicy, secretFetcher SecretFetcher) (*ResolvedPolicy, error) {
	if pol == nil {
		return nil, fmt.Errorf("ClusterModelTrustPolicy is nil")
	}
	mode := strings.TrimSpace(pol.Spec.EnforcementMode)
	if mode == "" {
		mode = v1alpha1.EnforcementModeEnforce
	}
	if mode != v1alpha1.EnforcementModeEnforce && mode != v1alpha1.EnforcementModeAudit {
		return nil, fmt.Errorf("invalid enforcementMode %q (must be Enforce or Audit)", mode)
	}
	if len(pol.Spec.AllowedRegistries) == 0 {
		return nil, fmt.Errorf("allowedRegistries cannot be empty")
	}

	ttlSec := pol.Spec.VerificationTTLSeconds
	if ttlSec <= 0 {
		ttlSec = 3600
	}
	maxAgeHours := pol.Spec.MaxEvidenceAgeHours
	if maxAgeHours < 0 {
		return nil, fmt.Errorf("maxEvidenceAgeHours cannot be negative")
	}

	msRules, msCanon, msFPs, err := resolveDomain(ctx, "modelSigners", pol.Spec.TrustDomains.ModelSigners, secretFetcher)
	if err != nil {
		return nil, err
	}
	evRules, evCanon, evFPs, err := resolveDomain(ctx, "evaluators", pol.Spec.TrustDomains.Evaluators, secretFetcher)
	if err != nil {
		return nil, err
	}
	raRules, raCanon, raFPs, err := resolveDomain(ctx, "releaseAuthorities", pol.Spec.TrustDomains.ReleaseAuthorities, secretFetcher)
	if err != nil {
		return nil, err
	}

	if pol.Spec.Requirements.RequireOMSSignature && len(msRules) == 0 {
		return nil, fmt.Errorf("requireOMSSignature is true, but trustDomains.modelSigners is empty")
	}
	if pol.Spec.Requirements.RequireEvaluationGatePass && len(evRules) == 0 {
		return nil, fmt.Errorf("requireEvaluationGatePass is true, but trustDomains.evaluators is empty")
	}
	if pol.Spec.Requirements.RequireReleaseQualification && len(raRules) == 0 {
		return nil, fmt.Errorf("requireReleaseQualification is true, but trustDomains.releaseAuthorities is empty")
	}

	sortedRegs := make([]string, 0, len(pol.Spec.AllowedRegistries))
	for _, r := range pol.Spec.AllowedRegistries {
		trimmed := strings.TrimSpace(r)
		if trimmed != "" {
			sortedRegs = append(sortedRegs, trimmed)
		}
	}
	sort.Strings(sortedRegs)

	sortedSAs := make([]string, 0, len(pol.Spec.Exemptions.ExemptServiceAccounts))
	for _, sa := range pol.Spec.Exemptions.ExemptServiceAccounts {
		trimmed := strings.TrimSpace(sa)
		if trimmed != "" {
			sortedSAs = append(sortedSAs, trimmed)
		}
	}
	sort.Strings(sortedSAs)

	canonDoc := canonicalPolicyDocument{
		SchemaVersion:          "cluster-model-trust-policy/v1alpha1",
		EnforcementMode:        mode,
		AllowedRegistries:      sortedRegs,
		VerificationTTLSeconds: ttlSec,
		MaxEvidenceAgeHours:    maxAgeHours,
		ModelSigners:           msCanon,
		Evaluators:             evCanon,
		ReleaseAuthorities:     raCanon,
		Requirements:           pol.Spec.Requirements,
		ExemptServiceAccounts:  sortedSAs,
		ExemptPodLabels:        pol.Spec.Exemptions.ExemptPodLabels,
	}

	polDigest, _, err := verify.CanonicalizeStructJCS(canonDoc)
	if err != nil {
		return nil, fmt.Errorf("compute JCS policyDigest: %w", err)
	}

	allCandidates := make([]verify.TrustedSignerRule, 0, len(msRules)+len(evRules)+len(raRules))
	allCandidates = append(allCandidates, msRules...)
	allCandidates = append(allCandidates, evRules...)
	allCandidates = append(allCandidates, raRules...)

	return &ResolvedPolicy{
		Name:                pol.Name,
		Generation:          pol.Generation,
		EnforcementMode:     mode,
		PolicyDigest:        polDigest,
		AllowedRegistries:   sortedRegs,
		VerificationTTL:     time.Duration(ttlSec) * time.Second,
		MaxEvidenceAge:      time.Duration(maxAgeHours) * time.Hour,
		Requirements:        pol.Spec.Requirements,
		Exemptions:          pol.Spec.Exemptions,
		ModelSigners:        msRules,
		Evaluators:          evRules,
		ReleaseAuthorities:  raRules,
		AllDomainCandidates: allCandidates,
		ModelSignerFPs:      msFPs,
		EvaluatorFPs:        evFPs,
		ReleaseAuthorityFPs: raFPs,
	}, nil
}

func resolveDomain(
	ctx context.Context,
	domainName string,
	specs []v1alpha1.TrustedPrincipalSpec,
	secretFetcher SecretFetcher,
) ([]verify.TrustedSignerRule, []canonicalSignerEntry, []string, error) {
	rules := make([]verify.TrustedSignerRule, 0, len(specs))
	canon := make([]canonicalSignerEntry, 0, len(specs))
	fps := make([]string, 0, len(specs))

	for _, s := range specs {
		id := strings.TrimSpace(s.ID)
		if id == "" {
			return nil, nil, nil, fmt.Errorf("%s entry has empty id", domainName)
		}
		var rawPEM []byte
		if strings.TrimSpace(s.PublicKeyPEM) != "" {
			rawPEM = []byte(s.PublicKeyPEM)
		} else if s.SecretRef != nil {
			if secretFetcher == nil {
				return nil, nil, nil, fmt.Errorf("%s[%s] references secret %s/%s, but no SecretFetcher is configured", domainName, id, s.SecretRef.Namespace, s.SecretRef.Name)
			}
			fetched, err := secretFetcher(ctx, s.SecretRef.Namespace, s.SecretRef.Name, s.SecretRef.Key)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("%s[%s] fetch secret %s/%s[%s]: %w", domainName, id, s.SecretRef.Namespace, s.SecretRef.Name, s.SecretRef.Key, err)
			}
			rawPEM = fetched
		} else {
			return nil, nil, nil, fmt.Errorf("%s[%s] must specify either publicKeyPEM or secretRef", domainName, id)
		}

		keyInfo, err := verify.ParsePublicKeyPEM(rawPEM, s.KeyID, s.Name)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s[%s] invalid public key: %w", domainName, id, err)
		}
		if expFP := strings.TrimSpace(s.KeyFingerprint); expFP != "" && expFP != keyInfo.Fingerprint {
			return nil, nil, nil, fmt.Errorf("%s[%s] key fingerprint mismatch: got %s, expected %s", domainName, id, keyInfo.Fingerprint, expFP)
		}

		rules = append(rules, verify.TrustedSignerRule{
			ID:                       id,
			Name:                     s.Name,
			Role:                     s.Role,
			AllowOperatorEndorsement: s.AllowOperatorEndorsement,
			KeyID:                    keyInfo.KeyID,
			KeyFingerprint:           keyInfo.Fingerprint,
			PublicKeyPEM:             rawPEM,
		})
		canon = append(canon, canonicalSignerEntry{
			ID:                       id,
			KeyID:                    keyInfo.KeyID,
			KeyFingerprint:           keyInfo.Fingerprint,
			Algorithm:                keyInfo.Algorithm,
			AllowOperatorEndorsement: s.AllowOperatorEndorsement,
		})
		fps = append(fps, keyInfo.Fingerprint)
	}

	sort.Slice(canon, func(i, j int) bool {
		if canon[i].ID != canon[j].ID {
			return canon[i].ID < canon[j].ID
		}
		return canon[i].KeyFingerprint < canon[j].KeyFingerprint
	})
	sort.Strings(fps)
	return rules, canon, fps, nil
}

// IsExempt checks whether a workload's bound ServiceAccount identity and Pod labels match an explicit policy exemption.
// Security invariant: Pod labels alone can NEVER grant an exemption because any workload creator can self-assign
// arbitrary Pod labels. A workload is exempt if and only if:
//  1. serviceAccountUsername matches at least one pattern in ExemptServiceAccounts, AND
//  2. If ExemptPodLabels is non-empty, podLabels also matches every key/value pair in ExemptPodLabels.
func (p *ResolvedPolicy) IsExempt(serviceAccountUsername string, podLabels map[string]string) bool {
	if p == nil || len(p.Exemptions.ExemptServiceAccounts) == 0 {
		return false
	}
	saMatched := false
	for _, pattern := range p.Exemptions.ExemptServiceAccounts {
		if pattern == serviceAccountUsername {
			saMatched = true
			break
		}
		if matched, err := filepath.Match(pattern, serviceAccountUsername); err == nil && matched {
			saMatched = true
			break
		}
	}
	if !saMatched {
		return false
	}
	for k, v := range p.Exemptions.ExemptPodLabels {
		if podLabels == nil || podLabels[k] != v {
			return false
		}
	}
	return true
}
