package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/devenes/ai-model-provenance-gate/pkg/verify"
	"github.com/devenes/gke-model-admission-controller/api/v1alpha1"
	admissionpkg "github.com/devenes/gke-model-admission-controller/internal/admission"
	"github.com/devenes/gke-model-admission-controller/internal/controller"
	"github.com/devenes/gke-model-admission-controller/internal/registry"
	"github.com/devenes/gke-model-admission-controller/internal/verification"
)

const (
	testNamespace       = "ai-serving"
	testRuntimeImage    = "us-west4-docker.pkg.dev/example-project/models/vllm-cpu@sha256:4444444444444444444444444444444444444444444444444444444444444444"
	testServingCfgHash  = "sha256:5555555555555555555555555555555555555555555555555555555555555555"
	testSuiteDigest     = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testDatasetDigest   = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testCandidateDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

type testCryptoKeys struct {
	msPriv, msPub []byte
	msFP          string
	evPriv, evPub []byte
	evFP          string
	raPriv, raPub []byte
	raFP          string
}

func generateTestKeys(t *testing.T) testCryptoKeys {
	t.Helper()
	msPriv, msPub, msFP, err := verify.GenerateEd25519KeyPairPEM()
	if err != nil {
		t.Fatal(err)
	}
	evPriv, evPub, evFP, err := verify.GenerateEd25519KeyPairPEM()
	if err != nil {
		t.Fatal(err)
	}
	raPriv, raPub, raFP, err := verify.GenerateEd25519KeyPairPEM()
	if err != nil {
		t.Fatal(err)
	}
	return testCryptoKeys{
		msPriv: msPriv, msPub: msPub, msFP: msFP,
		evPriv: evPriv, evPub: evPub, evFP: evFP,
		raPriv: raPriv, raPub: raPub, raFP: raFP,
	}
}

func buildTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := appsv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := batchv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func buildValidReportBytes(bundleID string, now time.Time) []byte {
	rep := map[string]any{
		"schemaVersion": "llm-release-gate.report/v1",
		"runId":         "run-e2e-001",
		"timestamp":     now.UTC().Format(time.RFC3339),
		"toolVersion":   "0.1.0",
		"suite": map[string]any{
			"name":    "gke-admission-suite",
			"version": "1.0.0",
			"digest":  testSuiteDigest,
		},
		"dataset": map[string]any{
			"digest":    testDatasetDigest,
			"caseCount": 2,
		},
		"graders": []map[string]any{
			{"type": "exact-match", "version": "1.0.0"},
		},
		"evaluator": map[string]any{
			"name":    "llm-release-gate",
			"version": "0.1.0",
		},
		"execution": map[string]any{
			"mode":        "native",
			"timeout":     "15s",
			"concurrency": 1,
			"repetitions": 1,
		},
		"candidate": map[string]any{
			"name":     "gemma-2b-it",
			"version":  "1.0.0",
			"provider": "openai-compatible",
			"model":    "gemma-2b-it",
			"digest":   testCandidateDigest,
			"metadata": map[string]string{
				"model_bundle_id": bundleID,
			},
		},
		"cases": []map[string]any{
			{"id": "c1", "repetition": 1, "status": "passed", "required": true, "assertions": []any{}},
			{"id": "c2", "repetition": 1, "status": "passed", "required": true, "assertions": []any{}},
		},
		"metrics": map[string]any{
			"totalPlannedCases": 2,
			"executedCases":     2,
			"passedCases":       2,
		},
		"gateStatus":       "PASS",
		"evidenceQuality":  "COMPLETE",
		"comparisonStatus": "NOT_APPLICABLE",
		"exitCode":         0,
		"decisionReasons": []map[string]any{
			{"code": "ALL_CHECKS_PASSED", "category": "policy", "severity": "info", "message": "all checks passed"},
		},
	}
	raw, _ := json.MarshalIndent(rep, "", "  ")
	return append(raw, '\n')
}

type publishedTestBundle struct {
	Published      registry.PublishedArtifactSet
	BundleID       string
	ManifestDigest string
	BOMHash        string
	WeightDigest   string
}

func publishFullBundleToRegistry(
	t *testing.T,
	regClient *registry.Client,
	regHost string,
	repo string,
	keys testCryptoKeys,
	now time.Time,
	opts verify.BuildManifestOptions,
) publishedTestBundle {
	t.Helper()
	cfgContent := []byte(`{"model_type":"gemma","architectures":["GemmaForCausalLM"]}`)
	weightContent := []byte("synthetic-safetensors-weight-payload-never-downloaded-by-controller")
	tokContent := []byte(`{"version":"1.0"}`)

	layerPayloads := []registry.LayerPayload{
		{Path: "config.json", Role: "model_config", Content: cfgContent},
		{Path: "model.safetensors", Role: "model_weights", Content: weightContent},
		{Path: "tokenizer.json", Role: "tokenizer_vocab", Content: tokContent},
	}
	if opts.HasLoRAAdapter {
		layerPayloads = []registry.LayerPayload{
			{Path: "adapter_config.json", Role: "lora_config", Content: []byte(`{"peft_type":"LORA","r":8}`)},
			{Path: "adapter_model.safetensors", Role: "lora_weights", Content: []byte("synthetic-lora-adapter-weights")},
		}
	}

	descriptors := make([]verify.FileEntry, 0, len(layerPayloads))
	for _, lp := range layerPayloads {
		descriptors = append(descriptors, verify.FileEntry{
			Path:   lp.Path,
			Role:   lp.Role,
			SHA256: verify.ComputeRawBytesDigest(lp.Content),
			Size:   int64(len(lp.Content)),
		})
	}

	opts.Timestamp = now.Format(time.RFC3339)
	if opts.ModelName == "" {
		opts.ModelName = "gemma-2b-it"
	}
	rawManifest, bm, err := verify.BuildBundleManifestBytes(descriptors, opts)
	if err != nil {
		t.Fatalf("BuildBundleManifestBytes: %v", err)
	}

	rawOMS, err := verify.SignOMSBytesFromDescriptors(descriptors, keys.msPriv, "ms-1")
	if err != nil {
		t.Fatalf("SignOMSBytesFromDescriptors: %v", err)
	}
	omsRes, err := verify.VerifyOMSBytes(rawOMS, descriptors, verify.OMSVerifyOptions{
		TrustedModelSigners: []verify.TrustedSignerRule{
			{ID: "ms-1", KeyID: "ms-1", KeyFingerprint: keys.msFP, PublicKeyPEM: keys.msPub},
		},
	})
	if err != nil {
		t.Fatalf("VerifyOMSBytes: %v", err)
	}

	rawBOM, bomHash, err := verify.GenerateBOMBytesFromDescriptors(descriptors, opts)
	if err != nil {
		t.Fatalf("GenerateBOMBytesFromDescriptors: %v", err)
	}

	rawReport := buildValidReportBytes(bm.BundleID, now)
	rawEvalAtt, err := verify.SignNativeEvaluationBytes(rawReport, bm.BundleID, keys.evPriv, "ev-1", now.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("SignNativeEvaluationBytes: %v", err)
	}
	evalRes, err := verify.VerifyEvaluationBytes(rawReport, rawEvalAtt, verify.EvalVerifyOptions{
		TrustedEvaluators: []verify.TrustedSignerRule{
			{ID: "ev-1", KeyID: "ev-1", KeyFingerprint: keys.evFP, PublicKeyPEM: keys.evPub},
		},
		ExpectedBundleID:       bm.BundleID,
		RequireArtifactBinding: true,
		RequireTrustedEvidence: true,
		AllowPostHocBinding:    false,
		Now:                    now,
	})
	if err != nil {
		t.Fatalf("VerifyEvaluationBytes: %v", err)
	}

	rawRelAtt, _, err := verify.SignReleaseQualificationBytes(verify.ReleaseBuildInput{
		Timestamp:           now.Format(time.RFC3339),
		Manifest:            bm,
		BOMCanonicalDigest:  bomHash,
		BOMSpecVersion:      "1.7",
		OMS:                 omsRes,
		Evaluation:          evalRes,
		RuntimeImage:        testRuntimeImage,
		ServingConfigDigest: testServingCfgHash,
	}, keys.raPriv, "ra-1")
	if err != nil {
		t.Fatalf("SignReleaseQualificationBytes: %v", err)
	}

	pub, err := regClient.PublishModelBundleAndEvidence(context.Background(), regHost, registry.ModelBundleArtifactInput{
		Repository:            repo,
		BundleID:              bm.BundleID,
		BundleManifestBytes:   rawManifest,
		LayerFiles:            layerPayloads,
		OMSSignatureBytes:     rawOMS,
		CycloneDXBOMBytes:     rawBOM,
		EvaluationAttestation: rawEvalAtt,
		EvaluationReportBytes: rawReport,
		ReleaseQualification:  rawRelAtt,
		CreatedTimestamp:      now.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("PublishModelBundleAndEvidence: %v", err)
	}

	return publishedTestBundle{
		Published:      pub,
		BundleID:       bm.BundleID,
		ManifestDigest: bm.ManifestDigest,
		BOMHash:        bomHash,
		WeightDigest:   descriptors[1].SHA256,
	}
}

func makeDefaultClusterPolicy(regHost string, keys testCryptoKeys) *v1alpha1.ClusterModelTrustPolicy {
	return &v1alpha1.ClusterModelTrustPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:       v1alpha1.DefaultClusterPolicyName,
			Generation: 1,
		},
		Spec: v1alpha1.ClusterModelTrustPolicySpec{
			EnforcementMode:        v1alpha1.EnforcementModeEnforce,
			AllowedRegistries:      []string{regHost + "/models/"},
			VerificationTTLSeconds: 3600,
			MaxEvidenceAgeHours:    24,
			TrustDomains: v1alpha1.TrustDomainsSpec{
				ModelSigners: []v1alpha1.TrustedPrincipalSpec{
					{ID: "model-signer-1", KeyID: "ms-1", KeyFingerprint: keys.msFP, PublicKeyPEM: string(keys.msPub)},
				},
				Evaluators: []v1alpha1.TrustedPrincipalSpec{
					{ID: "eval-signer-1", KeyID: "ev-1", KeyFingerprint: keys.evFP, PublicKeyPEM: string(keys.evPub)},
				},
				ReleaseAuthorities: []v1alpha1.TrustedPrincipalSpec{
					{ID: "rel-signer-1", KeyID: "ra-1", KeyFingerprint: keys.raFP, PublicKeyPEM: string(keys.raPub)},
				},
			},
			Requirements: v1alpha1.PolicyRequirementsSpec{
				RequireOMSSignature:           true,
				RequireCycloneDXBOM:           true,
				RequireEvaluationGatePass:     true,
				RequireNativeEvaluatorBinding: true,
				RejectPostHocBinding:          true,
				RequireReleaseQualification:   true,
				RequireImmutableRuntimeImage:  true,
				RequireServingConfigBinding:   true,
				RequireVerifiedLoRABase:       true,
				ExpectedSuiteDigest:           testSuiteDigest,
				ExpectedDatasetDigest:         testDatasetDigest,
			},
			Exemptions: v1alpha1.PolicyExemptionsSpec{
				ExemptServiceAccounts: []string{"system:serviceaccount:kube-system:*"},
			},
		},
	}
}

func TestEndToEndZeroWeightDownloadAndAllSixWorkloadKinds(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	mockReg := registry.NewInMemoryRegistry()
	defer mockReg.Close()

	regClient := registry.NewClient(mockReg.Server.Client())
	regClient.Scheme = "http"

	keys := generateTestKeys(t)
	bundleInfo := publishFullBundleToRegistry(t, regClient, mockReg.Host, "models/gemma-2b-it", keys, now, verify.BuildManifestOptions{
		ModelType:    "gemma",
		Architecture: "GemmaForCausalLM",
		ModelName:    "gemma-2b-it",
	})

	scheme := buildTestScheme(t)
	pol := makeDefaultClusterPolicy(mockReg.Host, keys)
	artifact := &v1alpha1.ModelArtifact{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  testNamespace,
			Name:       "gemma-2b-it",
			Generation: 1,
		},
		Spec: v1alpha1.ModelArtifactSpec{
			Artifact: v1alpha1.ArtifactSourceSpec{
				URI: bundleInfo.Published.ModelArtifactURI,
			},
			Identity: v1alpha1.BundleIdentitySpec{
				BundleID: bundleInfo.BundleID,
			},
			Runtime: v1alpha1.RuntimeBindingSpec{
				RuntimeImage:        testRuntimeImage,
				ServingConfigDigest: testServingCfgHash,
			},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.ClusterModelTrustPolicy{}, &v1alpha1.ModelArtifact{}).
		WithObjects(pol, artifact).
		Build()

	policyStore := controller.NewPolicyStore()
	polReconciler := &controller.ClusterModelTrustPolicyReconciler{
		Client: k8sClient,
		Scheme: scheme,
		Store:  policyStore,
	}
	if _, err := polReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: pol.Name}}); err != nil {
		t.Fatalf("ClusterModelTrustPolicy Reconcile: %v", err)
	}

	eng := verification.NewEngine(regClient)
	eng.NowFunc = func() time.Time { return now }

	artReconciler := &controller.ModelArtifactReconciler{
		Client:      k8sClient,
		Scheme:      scheme,
		PolicyStore: policyStore,
		Engine:      eng,
	}
	if _, err := artReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: artifact.Name}}); err != nil {
		t.Fatalf("ModelArtifact Reconcile: %v", err)
	}

	var updatedArt v1alpha1.ModelArtifact
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: artifact.Name}, &updatedArt); err != nil {
		t.Fatal(err)
	}
	if !updatedArt.Status.Verified {
		t.Fatalf("expected ModelArtifact to be Verified=true, got false: [%s] %s", updatedArt.Status.FailureReason, updatedArt.Status.FailureMessage)
	}

	// Prove ZERO weight-layer blob downloads occurred during controller verification!
	if dl := mockReg.BlobDownloadCount(bundleInfo.WeightDigest); dl != 0 {
		t.Fatalf("expected 0 downloads of model.safetensors weight blob, got %d", dl)
	}

	validator := admissionpkg.NewWorkloadValidator(k8sClient, k8sClient, policyStore)
	validator.NowFunc = func() time.Time { return now.Add(5 * time.Minute) }

	validPodMeta := metav1.ObjectMeta{
		Namespace: testNamespace,
		Name:      "gemma-serving",
		Annotations: map[string]string{
			v1alpha1.AnnotationModelArtifact:       "gemma-2b-it",
			v1alpha1.AnnotationServingConfigDigest: testServingCfgHash,
		},
	}
	validPodSpec := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:  "vllm",
				Image: testRuntimeImage,
			},
		},
	}

	// Test all 6 Kubernetes workload kinds via AdmissionReview handler
	workloads := []struct {
		kind string
		obj  any
	}{
		{
			kind: "Pod",
			obj:  corev1.Pod{ObjectMeta: validPodMeta, Spec: validPodSpec},
		},
		{
			kind: "Deployment",
			obj: appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "gemma-deploy"},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{ObjectMeta: validPodMeta, Spec: validPodSpec},
				},
			},
		},
		{
			kind: "StatefulSet",
			obj: appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "gemma-sts"},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{ObjectMeta: validPodMeta, Spec: validPodSpec},
				},
			},
		},
		{
			kind: "DaemonSet",
			obj: appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "gemma-ds"},
				Spec: appsv1.DaemonSetSpec{
					Template: corev1.PodTemplateSpec{ObjectMeta: validPodMeta, Spec: validPodSpec},
				},
			},
		},
		{
			kind: "Job",
			obj: batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "gemma-job"},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{ObjectMeta: validPodMeta, Spec: validPodSpec},
				},
			},
		},
		{
			kind: "CronJob",
			obj: batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "gemma-cron"},
				Spec: batchv1.CronJobSpec{
					JobTemplate: batchv1.JobTemplateSpec{
						Spec: batchv1.JobSpec{
							Template: corev1.PodTemplateSpec{ObjectMeta: validPodMeta, Spec: validPodSpec},
						},
					},
				},
			},
		},
	}

	for _, tc := range workloads {
		raw, err := json.Marshal(tc.obj)
		if err != nil {
			t.Fatal(err)
		}
		resp := validator.Handle(ctx, admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				UID:       types.UID("uid-" + tc.kind),
				Kind:      metav1.GroupVersionKind{Kind: tc.kind},
				Namespace: testNamespace,
				Name:      "test-" + tc.kind,
				Operation: admissionv1.Create,
				Object:    runtime.RawExtension{Raw: raw},
			},
		})
		if !resp.Allowed {
			t.Fatalf("expected valid %s to be ALLOWED, got DENIED: %v", tc.kind, resp.Result)
		}
	}

	// Test Default-Deny: Deployment in enforced namespace WITHOUT trust.gke.dev/model-artifact must be DENIED
	unannotatedDep := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "rogue-deploy"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace},
				Spec:       validPodSpec,
			},
		},
	}
	rawRogue, _ := json.Marshal(unannotatedDep)
	rogueResp := validator.Handle(ctx, admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       "uid-rogue",
			Kind:      metav1.GroupVersionKind{Kind: "Deployment"},
			Namespace: testNamespace,
			Name:      "rogue-deploy",
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: rawRogue},
		},
	})
	if rogueResp.Allowed || string(rogueResp.Result.Reason) != v1alpha1.ReasonMissingModelArtifactReference {
		t.Fatalf("expected unannotated Deployment to be denied with %s, got allowed=%v result=%+v",
			v1alpha1.ReasonMissingModelArtifactReference, rogueResp.Allowed, rogueResp.Result)
	}
}

func TestSecurityAndTamperMatrix(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	mockReg := registry.NewInMemoryRegistry()
	defer mockReg.Close()

	regClient := registry.NewClient(mockReg.Server.Client())
	regClient.Scheme = "http"

	keys := generateTestKeys(t)
	baseBundle := publishFullBundleToRegistry(t, regClient, mockReg.Host, "models/gemma-base", keys, now, verify.BuildManifestOptions{
		ModelType:    "gemma",
		Architecture: "GemmaForCausalLM",
		ModelName:    "gemma-base",
	})

	scheme := buildTestScheme(t)
	pol := makeDefaultClusterPolicy(mockReg.Host, keys)
	baseArt := &v1alpha1.ModelArtifact{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "gemma-base", Generation: 1},
		Spec: v1alpha1.ModelArtifactSpec{
			Artifact: v1alpha1.ArtifactSourceSpec{URI: baseBundle.Published.ModelArtifactURI},
			Identity: v1alpha1.BundleIdentitySpec{BundleID: baseBundle.BundleID},
			Runtime: v1alpha1.RuntimeBindingSpec{
				RuntimeImage:        testRuntimeImage,
				ServingConfigDigest: testServingCfgHash,
			},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.ClusterModelTrustPolicy{}, &v1alpha1.ModelArtifact{}).
		WithObjects(pol, baseArt).
		Build()

	policyStore := controller.NewPolicyStore()
	polRec := &controller.ClusterModelTrustPolicyReconciler{Client: k8sClient, Scheme: scheme, Store: policyStore}
	_, _ = polRec.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: pol.Name}})

	eng := verification.NewEngine(regClient)
	eng.NowFunc = func() time.Time { return now }
	artRec := &controller.ModelArtifactReconciler{Client: k8sClient, Scheme: scheme, PolicyStore: policyStore, Engine: eng}
	_, _ = artRec.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: baseArt.Name}})

	validator := admissionpkg.NewWorkloadValidator(k8sClient, k8sClient, policyStore)
	validator.NowFunc = func() time.Time { return now }

	// 1. LoRA Adapter bound to verified base model succeeds; LoRA bound to wrong base fails
	loraValid := publishFullBundleToRegistry(t, regClient, mockReg.Host, "models/gemma-lora", keys, now, verify.BuildManifestOptions{
		PrimaryFormat:        "safetensors",
		HasLoRAAdapter:       true,
		DeclaredBaseModel:    "gemma-base",
		VerifiedBaseBundleID: baseBundle.BundleID,
		ModelName:            "gemma-lora",
	})
	loraArt := &v1alpha1.ModelArtifact{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "gemma-lora", Generation: 1},
		Spec: v1alpha1.ModelArtifactSpec{
			Artifact: v1alpha1.ArtifactSourceSpec{URI: loraValid.Published.ModelArtifactURI},
			Identity: v1alpha1.BundleIdentitySpec{BundleID: loraValid.BundleID},
			Runtime: v1alpha1.RuntimeBindingSpec{
				RuntimeImage:        testRuntimeImage,
				ServingConfigDigest: testServingCfgHash,
				BaseModelArtifact:   "gemma-base",
			},
		},
	}
	if err := k8sClient.Create(ctx, loraArt); err != nil {
		t.Fatal(err)
	}
	_, _ = artRec.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: loraArt.Name}})

	var checkedLora v1alpha1.ModelArtifact
	_ = k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: loraArt.Name}, &checkedLora)
	if !checkedLora.Status.Verified {
		t.Fatalf("expected valid LoRA ModelArtifact to verify, got [%s] %s", checkedLora.Status.FailureReason, checkedLora.Status.FailureMessage)
	}

	// 2. Post-Hoc Evaluation Binding must be rejected
	postHocKeys := keys
	rawPostHocAtt, err := verify.SignPostHocEvaluationBytes(buildValidReportBytes(baseBundle.BundleID, now), baseBundle.BundleID, postHocKeys.evPriv, "ev-1", now.Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	rawManifest, _, _ := verify.BuildBundleManifestBytes(baseBundle.Published.ObservedDescriptors, verify.BuildManifestOptions{
		ModelType: "gemma", Architecture: "GemmaForCausalLM", Timestamp: now.Format(time.RFC3339),
	})
	rawOMS, _ := verify.SignOMSBytesFromDescriptors(baseBundle.Published.ObservedDescriptors, keys.msPriv, "ms-1")
	rawBOM, _, _ := verify.GenerateBOMBytesFromDescriptors(baseBundle.Published.ObservedDescriptors, verify.BuildManifestOptions{
		ModelType: "gemma", Architecture: "GemmaForCausalLM", Timestamp: now.Format(time.RFC3339),
	})
	pubPostHoc, err := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository:          "models/gemma-posthoc",
		BundleID:            baseBundle.BundleID,
		BundleManifestBytes: rawManifest,
		LayerFiles: []registry.LayerPayload{
			{Path: "config.json", Role: "model_config", Content: []byte(`{"model_type":"gemma","architectures":["GemmaForCausalLM"]}`)},
			{Path: "model.safetensors", Role: "model_weights", Content: []byte("synthetic-safetensors-weight-payload-never-downloaded-by-controller")},
			{Path: "tokenizer.json", Role: "tokenizer_vocab", Content: []byte(`{"version":"1.0"}`)},
		},
		OMSSignatureBytes:     rawOMS,
		CycloneDXBOMBytes:     rawBOM,
		EvaluationAttestation: rawPostHocAtt,
		EvaluationReportBytes: buildValidReportBytes(baseBundle.BundleID, now),
		CreatedTimestamp:      now.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	postHocArt := &v1alpha1.ModelArtifact{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "gemma-posthoc", Generation: 1},
		Spec: v1alpha1.ModelArtifactSpec{
			Artifact: v1alpha1.ArtifactSourceSpec{URI: pubPostHoc.ModelArtifactURI},
			Identity: v1alpha1.BundleIdentitySpec{BundleID: baseBundle.BundleID},
		},
	}
	_ = k8sClient.Create(ctx, postHocArt)
	_, _ = artRec.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: postHocArt.Name}})
	_ = k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: postHocArt.Name}, postHocArt)
	if postHocArt.Status.Verified || postHocArt.Status.FailureReason != v1alpha1.ReasonPostHocEvaluationRejected {
		t.Fatalf("expected POST_HOC_OPERATOR evaluation to fail with %s, got verified=%v reason=%s",
			v1alpha1.ReasonPostHocEvaluationRejected, postHocArt.Status.Verified, postHocArt.Status.FailureReason)
	}

	// 3. Mutable container image or mismatched serving config digest rejected at admission
	decMutable := validator.EvaluateWorkload(ctx, "Pod", testNamespace, "bad-img-pod", "system:serviceaccount:ai-serving:default",
		map[string]string{
			v1alpha1.AnnotationModelArtifact:       "gemma-base",
			v1alpha1.AnnotationServingConfigDigest: testServingCfgHash,
		},
		nil,
		corev1.PodSpec{
			Containers: []corev1.Container{{Name: "vllm", Image: "us-west4-docker.pkg.dev/example-project/models/vllm-cpu:latest"}},
		},
	)
	if decMutable.Allowed || decMutable.Reason != v1alpha1.ReasonMutableRuntimeImage {
		t.Fatalf("expected mutable runtime image to be denied with %s, got %+v", v1alpha1.ReasonMutableRuntimeImage, decMutable)
	}

	// 4. TTL Expiry Rejection
	validator.NowFunc = func() time.Time { return now.Add(2 * time.Hour) }
	decExpired := validator.EvaluateWorkload(ctx, "Pod", testNamespace, "expired-pod", "system:serviceaccount:ai-serving:default",
		map[string]string{
			v1alpha1.AnnotationModelArtifact:       "gemma-base",
			v1alpha1.AnnotationServingConfigDigest: testServingCfgHash,
		},
		nil,
		corev1.PodSpec{
			Containers: []corev1.Container{{Name: "vllm", Image: testRuntimeImage}},
		},
	)
	if decExpired.Allowed || decExpired.Reason != v1alpha1.ReasonVerificationExpired {
		t.Fatalf("expected expired ModelArtifact verification to be denied with %s, got %+v", v1alpha1.ReasonVerificationExpired, decExpired)
	}
	validator.NowFunc = func() time.Time { return now }

	// 5. PolicyDigest change invalidates previously verified ModelArtifact immediately
	var currentPol v1alpha1.ClusterModelTrustPolicy
	_ = k8sClient.Get(ctx, types.NamespacedName{Name: pol.Name}, &currentPol)
	currentPol.Spec.Exemptions.ExemptServiceAccounts = append(currentPol.Spec.Exemptions.ExemptServiceAccounts, "system:serviceaccount:monitoring:prometheus")
	currentPol.Generation = 2
	_ = k8sClient.Update(ctx, &currentPol)

	decPolicyChanged := validator.EvaluateWorkload(ctx, "Pod", testNamespace, "stale-policy-pod", "system:serviceaccount:ai-serving:default",
		map[string]string{
			v1alpha1.AnnotationModelArtifact:       "gemma-base",
			v1alpha1.AnnotationServingConfigDigest: testServingCfgHash,
		},
		nil,
		corev1.PodSpec{
			Containers: []corev1.Container{{Name: "vllm", Image: testRuntimeImage}},
		},
	)
	if decPolicyChanged.Allowed || decPolicyChanged.Reason != v1alpha1.ReasonPolicyDigestMismatch {
		t.Fatalf("expected policy modification to invalidate cached ModelArtifact with %s, got %+v", v1alpha1.ReasonPolicyDigestMismatch, decPolicyChanged)
	}

	// 6. Audit Mode emits warning and allows
	_ = k8sClient.Get(ctx, types.NamespacedName{Name: pol.Name}, &currentPol)
	currentPol.Spec.EnforcementMode = v1alpha1.EnforcementModeAudit
	_ = k8sClient.Update(ctx, &currentPol)

	decAudit := validator.EvaluateWorkload(ctx, "Pod", testNamespace, "audit-pod", "system:serviceaccount:ai-serving:default",
		nil, // Missing annotation in Audit mode
		nil,
		corev1.PodSpec{
			Containers: []corev1.Container{{Name: "vllm", Image: testRuntimeImage}},
		},
	)
	if !decAudit.Allowed || len(decAudit.Warnings) == 0 || decAudit.Reason != v1alpha1.ReasonMissingModelArtifactReference {
		t.Fatalf("expected Audit mode to allow with warning for %s, got %+v", fmt.Sprintf(v1alpha1.ReasonMissingModelArtifactReference), decAudit)
	}
}
