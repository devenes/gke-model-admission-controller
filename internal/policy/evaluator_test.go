package policy_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/devenes/ai-model-provenance-gate/pkg/verify"
	"github.com/devenes/gke-model-admission-controller/api/v1alpha1"
	"github.com/devenes/gke-model-admission-controller/internal/policy"
)

func TestResolvePolicyDigestDeterminismAndInvalidation(t *testing.T) {
	ctx := context.Background()
	_, msPub, msFP, _ := verify.GenerateEd25519KeyPairPEM()
	_, evPub, evFP, _ := verify.GenerateEd25519KeyPairPEM()
	_, raPub, raFP, _ := verify.GenerateEd25519KeyPairPEM()

	pol := &v1alpha1.ClusterModelTrustPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "default", Generation: 1},
		Spec: v1alpha1.ClusterModelTrustPolicySpec{
			EnforcementMode:   "Enforce",
			AllowedRegistries: []string{"us-west4-docker.pkg.dev/example-project/models/"},
			TrustDomains: v1alpha1.TrustDomainsSpec{
				ModelSigners: []v1alpha1.TrustedPrincipalSpec{
					{ID: "ms-1", KeyFingerprint: msFP, PublicKeyPEM: string(msPub)},
				},
				Evaluators: []v1alpha1.TrustedPrincipalSpec{
					{ID: "ev-1", KeyFingerprint: evFP, PublicKeyPEM: string(evPub)},
				},
				ReleaseAuthorities: []v1alpha1.TrustedPrincipalSpec{
					{ID: "ra-1", KeyFingerprint: raFP, PublicKeyPEM: string(raPub)},
				},
			},
			Requirements: v1alpha1.PolicyRequirementsSpec{
				RequireOMSSignature:         true,
				RequireEvaluationGatePass:   true,
				RequireReleaseQualification: true,
			},
		},
	}

	res1, err := policy.Resolve(ctx, pol, nil)
	if err != nil {
		t.Fatalf("policy.Resolve: %v", err)
	}
	res2, err := policy.Resolve(ctx, pol, nil)
	if err != nil {
		t.Fatalf("policy.Resolve second call: %v", err)
	}
	if res1.PolicyDigest != res2.PolicyDigest {
		t.Fatalf("expected deterministic PolicyDigest, got %s vs %s", res1.PolicyDigest, res2.PolicyDigest)
	}

	// Modifying exemptions must change PolicyDigest
	pol.Spec.Exemptions.ExemptServiceAccounts = []string{"system:serviceaccount:kube-system:coredns"}
	res3, err := policy.Resolve(ctx, pol, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res3.PolicyDigest == res1.PolicyDigest {
		t.Fatal("expected PolicyDigest to change when exemptions are modified")
	}

	// Verify that self-assigned Pod labels alone NEVER grant an exemption when ServiceAccount is not exempt
	pol.Spec.Exemptions.ExemptServiceAccounts = []string{"system:serviceaccount:kube-system:*"}
	pol.Spec.Exemptions.ExemptPodLabels = map[string]string{"trust.gke.dev/exempt": "true"}
	res4, err := policy.Resolve(ctx, pol, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res4.IsExempt("system:serviceaccount:ai-serving:default", map[string]string{"trust.gke.dev/exempt": "true"}) {
		t.Fatal("expected self-assigned Pod label with non-exempt ServiceAccount to be rejected")
	}
	if res4.IsExempt("system:serviceaccount:kube-system:coredns", nil) {
		t.Fatal("expected missing required ExemptPodLabels to be rejected when ExemptPodLabels is configured")
	}
	if !res4.IsExempt("system:serviceaccount:kube-system:coredns", map[string]string{"trust.gke.dev/exempt": "true"}) {
		t.Fatal("expected matching ExemptServiceAccounts AND ExemptPodLabels to be exempt")
	}
}
