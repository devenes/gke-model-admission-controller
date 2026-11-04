package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
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

func TestFull28NegativeSecurityMatrix(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	mockReg := registry.NewInMemoryRegistry()
	defer mockReg.Close()

	regClient := registry.NewClient(mockReg.Server.Client())
	regClient.Scheme = "http"

	keys := generateTestKeys(t)
	rogueKeys := generateTestKeys(t)

	baseBundle := publishFullBundleToRegistry(t, regClient, mockReg.Host, "models/gemma-matrix-base", keys, now, verify.BuildManifestOptions{
		ModelType:    "gemma",
		Architecture: "GemmaForCausalLM",
		ModelName:    "gemma-matrix-base",
	})

	scheme := buildTestScheme(t)
	pol := makeDefaultClusterPolicy(mockReg.Host, keys)
	baseArt := &v1alpha1.ModelArtifact{
		ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "gemma-matrix-base", Generation: 1},
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

	validPodSpec := corev1.PodSpec{
		Containers: []corev1.Container{{Name: "vllm", Image: testRuntimeImage}},
	}

	// Helper to reconcile a candidate ModelArtifact and assert it fails with expectedReason
	assertArtifactDenied := func(caseID, name string, spec v1alpha1.ModelArtifactSpec, expectedReason string) {
		t.Helper()
		art := &v1alpha1.ModelArtifact{
			ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: name, Generation: 1},
			Spec:       spec,
		}
		if err := k8sClient.Create(ctx, art); err != nil {
			t.Fatalf("[%s] create ModelArtifact: %v", caseID, err)
		}
		if _, err := artRec.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: testNamespace, Name: name}}); err != nil {
			t.Fatalf("[%s] reconcile ModelArtifact: %v", caseID, err)
		}
		var updated v1alpha1.ModelArtifact
		_ = k8sClient.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: name}, &updated)
		if updated.Status.Verified {
			t.Fatalf("[%s] expected ModelArtifact %s to fail verification (%s), but Verified=true", caseID, name, expectedReason)
		}
		if updated.Status.FailureReason != expectedReason {
			t.Fatalf("[%s] expected FailureReason=%s, got %s (%s)", caseID, expectedReason, updated.Status.FailureReason, updated.Status.FailureMessage)
		}
		dec := validator.EvaluateWorkload(ctx, "Pod", testNamespace, "pod-"+name, "system:serviceaccount:ai-serving:default",
			map[string]string{
				v1alpha1.AnnotationModelArtifact:       name,
				v1alpha1.AnnotationServingConfigDigest: testServingCfgHash,
			},
			nil, validPodSpec)
		if dec.Allowed || dec.Reason != expectedReason {
			t.Fatalf("[%s] expected workload admission DENY (%s), got %+v", caseID, expectedReason, dec)
		}
	}

	defaultLayers := []registry.LayerPayload{
		{Path: "config.json", Role: "model_config", Content: []byte(`{"model_type":"gemma","architectures":["GemmaForCausalLM"]}`)},
		{Path: "model.safetensors", Role: "model_weights", Content: []byte("synthetic-safetensors-weight-payload-never-downloaded-by-controller")},
		{Path: "tokenizer.json", Role: "tokenizer_vocab", Content: []byte(`{"version":"1.0"}`)},
	}
	descriptors := baseBundle.Published.ObservedDescriptors
	rawManifest, bm, _ := verify.BuildBundleManifestBytes(descriptors, verify.BuildManifestOptions{
		ModelType: "gemma", Architecture: "GemmaForCausalLM", ModelName: "gemma-matrix-base", Timestamp: now.Format(time.RFC3339),
	})
	rawOMS, _ := verify.SignOMSBytesFromDescriptors(descriptors, keys.msPriv, "ms-1")
	omsRes, _ := verify.VerifyOMSBytes(rawOMS, descriptors, verify.OMSVerifyOptions{
		TrustedModelSigners: []verify.TrustedSignerRule{{ID: "ms-1", KeyID: "ms-1", KeyFingerprint: keys.msFP, PublicKeyPEM: keys.msPub}},
	})
	rawBOM, bomHash, _ := verify.GenerateBOMBytesFromDescriptors(descriptors, verify.BuildManifestOptions{
		ModelType: "gemma", Architecture: "GemmaForCausalLM", ModelName: "gemma-matrix-base", Timestamp: now.Format(time.RFC3339),
	})
	rawReport := buildValidReportBytes(bm.BundleID, now)
	rawEvalAtt, _ := verify.SignNativeEvaluationBytes(rawReport, bm.BundleID, keys.evPriv, "ev-1", now.Format(time.RFC3339))
	evalRes, _ := verify.VerifyEvaluationBytes(rawReport, rawEvalAtt, verify.EvalVerifyOptions{
		TrustedEvaluators:      []verify.TrustedSignerRule{{ID: "ev-1", KeyID: "ev-1", KeyFingerprint: keys.evFP, PublicKeyPEM: keys.evPub}},
		ExpectedBundleID:       bm.BundleID,
		RequireArtifactBinding: true,
		RequireTrustedEvidence: true,
		Now:                    now,
	})
	rawRelAtt, _, _ := verify.SignReleaseQualificationBytes(verify.ReleaseBuildInput{
		Timestamp: now.Format(time.RFC3339), Manifest: bm, BOMCanonicalDigest: bomHash, BOMSpecVersion: "1.7",
		OMS: omsRes, Evaluation: evalRes, RuntimeImage: testRuntimeImage, ServingConfigDigest: testServingCfgHash,
	}, keys.raPriv, "ra-1")

	// N-01: Missing ModelArtifact reference in enforced namespace
	dec01 := validator.EvaluateWorkload(ctx, "Pod", testNamespace, "n01-pod", "system:serviceaccount:ai-serving:default", nil, nil, validPodSpec)
	if dec01.Allowed || dec01.Reason != v1alpha1.ReasonMissingModelArtifactReference {
		t.Fatalf("[N-01] expected %s, got %+v", v1alpha1.ReasonMissingModelArtifactReference, dec01)
	}

	// N-02: Non-existent ModelArtifact
	dec02 := validator.EvaluateWorkload(ctx, "Pod", testNamespace, "n02-pod", "system:serviceaccount:ai-serving:default",
		map[string]string{v1alpha1.AnnotationModelArtifact: "non-existent"}, nil, validPodSpec)
	if dec02.Allowed || dec02.Reason != v1alpha1.ReasonModelArtifactNotFound {
		t.Fatalf("[N-02] expected %s, got %+v", v1alpha1.ReasonModelArtifactNotFound, dec02)
	}

	// N-03: Mutable OCI tag without digest
	assertArtifactDenied("N-03", "n03-mutable-tag", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: mockReg.Host + "/models/gemma:latest"},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonInvalidArtifactURI)

	// N-04: Untrusted registry
	assertArtifactDenied("N-04", "n04-untrusted-reg", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: "docker.io/library/gemma@" + baseBundle.Published.ModelManifestDigest},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonUntrustedRegistry)

	// N-05: Missing OMS signature referrer
	pub05, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n05", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: defaultLayers,
		CycloneDXBOMBytes: rawBOM, EvaluationAttestation: rawEvalAtt, EvaluationReportBytes: rawReport, ReleaseQualification: rawRelAtt,
	})
	assertArtifactDenied("N-05", "n05-no-oms", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub05.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonOMSSignatureInvalid)

	// N-06: Missing CycloneDX BOM referrer
	pub06, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n06", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: defaultLayers,
		OMSSignatureBytes: rawOMS, EvaluationAttestation: rawEvalAtt, EvaluationReportBytes: rawReport, ReleaseQualification: rawRelAtt,
	})
	assertArtifactDenied("N-06", "n06-no-bom", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub06.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonBOMVerificationFailed)

	// N-07: Missing evaluation attestation referrer
	pub07, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n07", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: defaultLayers,
		OMSSignatureBytes: rawOMS, CycloneDXBOMBytes: rawBOM, ReleaseQualification: rawRelAtt,
	})
	assertArtifactDenied("N-07", "n07-no-eval", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub07.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonEvaluationGateFailed)

	// N-08: Duplicate/ambiguous unpinned OMS referrers
	pub08, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n08", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: defaultLayers,
		OMSSignatureBytes: rawOMS, CycloneDXBOMBytes: rawBOM, EvaluationAttestation: rawEvalAtt, EvaluationReportBytes: rawReport, ReleaseQualification: rawRelAtt,
	})
	// Push a second OMS referrer with different annotation timestamp to the same subject
	omsBlob08, _ := regClient.PushBlob(ctx, mockReg.Host, "models/n08", rawOMS)
	omsBlob08.MediaType = registry.MediaTypeDSSELayer
	_, _, _ = regClient.PushManifest(ctx, mockReg.Host, "models/n08", registry.Manifest{
		SchemaVersion: 2, MediaType: registry.OCIManifestSchemaV2, ArtifactType: registry.ArtifactTypeOMSSignature,
		Config:  registry.Descriptor{MediaType: registry.OCIEmptyConfigMediaType, Digest: registry.OCIEmptyConfigDigest, Size: 2},
		Layers:  []registry.Descriptor{omsBlob08},
		Subject: &registry.Descriptor{MediaType: registry.OCIManifestSchemaV2, Digest: pub08.ModelManifestDigest, Size: 500},
		Annotations: map[string]string{
			registry.AnnotationCreatedTimestamp: now.Add(time.Minute).Format(time.RFC3339),
		},
	})
	assertArtifactDenied("N-08", "n08-dup-referrer", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub08.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonOMSSignatureInvalid)

	// N-09: Missing release qualification attestation referrer
	pub09, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n09", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: defaultLayers,
		OMSSignatureBytes: rawOMS, CycloneDXBOMBytes: rawBOM, EvaluationAttestation: rawEvalAtt, EvaluationReportBytes: rawReport,
	})
	assertArtifactDenied("N-09", "n09-no-relqual", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub09.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonReleaseQualificationFailed)

	// N-10: Tampered weight digest in OCI layer vs bundle-manifest.json
	tamperedWeightLayers := []registry.LayerPayload{
		defaultLayers[0],
		{Path: "model.safetensors", Role: "model_weights", Content: []byte("TAMPERED-WEIGHT-BYTES")},
		defaultLayers[2],
	}
	pub10, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n10", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: tamperedWeightLayers,
		OMSSignatureBytes: rawOMS, CycloneDXBOMBytes: rawBOM, EvaluationAttestation: rawEvalAtt, EvaluationReportBytes: rawReport, ReleaseQualification: rawRelAtt,
	})
	assertArtifactDenied("N-10", "n10-tampered-weights", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub10.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonInvalidBundleManifest)

	// N-11: Tampered config.json digest
	tamperedCfgLayers := []registry.LayerPayload{
		{Path: "config.json", Role: "model_config", Content: []byte(`{"model_type":"tampered"}`)},
		defaultLayers[1],
		defaultLayers[2],
	}
	pub11, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n11", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: tamperedCfgLayers,
		OMSSignatureBytes: rawOMS, CycloneDXBOMBytes: rawBOM, EvaluationAttestation: rawEvalAtt, EvaluationReportBytes: rawReport, ReleaseQualification: rawRelAtt,
	})
	assertArtifactDenied("N-11", "n11-tampered-config", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub11.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonInvalidBundleManifest)

	// N-12: Tampered tokenizer.json digest
	tamperedTokLayers := []registry.LayerPayload{
		defaultLayers[0],
		defaultLayers[1],
		{Path: "tokenizer.json", Role: "tokenizer_vocab", Content: []byte(`{"version":"tampered"}`)},
	}
	pub12, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n12", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: tamperedTokLayers,
		OMSSignatureBytes: rawOMS, CycloneDXBOMBytes: rawBOM, EvaluationAttestation: rawEvalAtt, EvaluationReportBytes: rawReport, ReleaseQualification: rawRelAtt,
	})
	assertArtifactDenied("N-12", "n12-tampered-tok", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub12.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonInvalidBundleManifest)

	// N-13: Undeclared extra file layer in OCI manifest
	extraFileLayers := append([]registry.LayerPayload{}, defaultLayers...)
	extraFileLayers = append(extraFileLayers, registry.LayerPayload{
		Path: "README.md", Role: "documentation", Content: []byte("extra undeclared file"),
	})
	pub13, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n13", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: extraFileLayers,
		OMSSignatureBytes: rawOMS, CycloneDXBOMBytes: rawBOM, EvaluationAttestation: rawEvalAtt, EvaluationReportBytes: rawReport, ReleaseQualification: rawRelAtt,
	})
	assertArtifactDenied("N-13", "n13-undeclared-layer", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub13.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonInvalidBundleManifest)

	// N-14: Untrusted model signer key
	rogueOMS, _ := verify.SignOMSBytesFromDescriptors(descriptors, rogueKeys.msPriv, "rogue-ms")
	pub14, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n14", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: defaultLayers,
		OMSSignatureBytes: rogueOMS, CycloneDXBOMBytes: rawBOM, EvaluationAttestation: rawEvalAtt, EvaluationReportBytes: rawReport, ReleaseQualification: rawRelAtt,
	})
	assertArtifactDenied("N-14", "n14-untrusted-ms", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub14.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonUntrustedModelSigner)

	// N-15: Untrusted evaluator key
	rogueEvalAtt, _ := verify.SignNativeEvaluationBytes(rawReport, bm.BundleID, rogueKeys.evPriv, "rogue-ev", now.Format(time.RFC3339))
	pub15, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n15", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: defaultLayers,
		OMSSignatureBytes: rawOMS, CycloneDXBOMBytes: rawBOM, EvaluationAttestation: rogueEvalAtt, EvaluationReportBytes: rawReport, ReleaseQualification: rawRelAtt,
	})
	assertArtifactDenied("N-15", "n15-untrusted-ev", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub15.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonUntrustedEvaluator)

	// N-16: Untrusted release authority key
	rogueRelAtt, _, _ := verify.SignReleaseQualificationBytes(verify.ReleaseBuildInput{
		Timestamp: now.Format(time.RFC3339), Manifest: bm, BOMCanonicalDigest: bomHash, BOMSpecVersion: "1.7",
		OMS: omsRes, Evaluation: evalRes, RuntimeImage: testRuntimeImage, ServingConfigDigest: testServingCfgHash,
	}, rogueKeys.raPriv, "rogue-ra")
	pub16, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n16", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: defaultLayers,
		OMSSignatureBytes: rawOMS, CycloneDXBOMBytes: rawBOM, EvaluationAttestation: rawEvalAtt, EvaluationReportBytes: rawReport, ReleaseQualification: rogueRelAtt,
	})
	assertArtifactDenied("N-16", "n16-untrusted-ra", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub16.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonUntrustedReleaseAuthority)

	// N-17: Cross-Domain Key Confusion (OMS signed by trusted Evaluator key ev-1)
	crossDomainOMS, _ := verify.SignOMSBytesFromDescriptors(descriptors, keys.evPriv, "ev-1")
	pub17, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n17", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: defaultLayers,
		OMSSignatureBytes: crossDomainOMS, CycloneDXBOMBytes: rawBOM, EvaluationAttestation: rawEvalAtt, EvaluationReportBytes: rawReport, ReleaseQualification: rawRelAtt,
	})
	assertArtifactDenied("N-17", "n17-cross-domain-key", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub17.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonUntrustedModelSigner)

	// N-18: Post-hoc evaluation binding (POST_HOC_OPERATOR)
	postHocAtt, _ := verify.SignPostHocEvaluationBytes(rawReport, bm.BundleID, keys.evPriv, "ev-1", now.Format(time.RFC3339))
	pub18, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n18", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: defaultLayers,
		OMSSignatureBytes: rawOMS, CycloneDXBOMBytes: rawBOM, EvaluationAttestation: postHocAtt, EvaluationReportBytes: rawReport, ReleaseQualification: rawRelAtt,
	})
	assertArtifactDenied("N-18", "n18-posthoc-eval", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub18.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonPostHocEvaluationRejected)

	// N-19: Evaluation gateStatus = FAIL
	var failRepMap map[string]any
	_ = json.Unmarshal(rawReport, &failRepMap)
	failRepMap["gateStatus"] = "FAIL"
	failRepMap["exitCode"] = 2
	rawFailRep, _ := json.MarshalIndent(failRepMap, "", "  ")
	failEvalAtt, _ := verify.SignNativeEvaluationBytes(rawFailRep, bm.BundleID, keys.evPriv, "ev-1", now.Format(time.RFC3339))
	pub19, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n19", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: defaultLayers,
		OMSSignatureBytes: rawOMS, CycloneDXBOMBytes: rawBOM, EvaluationAttestation: failEvalAtt, EvaluationReportBytes: rawFailRep, ReleaseQualification: rawRelAtt,
	})
	assertArtifactDenied("N-19", "n19-eval-fail", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub19.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonEvaluationGateFailed)

	// N-20: Evaluation bound to different bundle_id
	otherBundleID := "sha256:9999999999999999999999999999999999999999999999999999999999999999"
	otherReport := buildValidReportBytes(otherBundleID, now)
	otherEvalAtt, _ := verify.SignNativeEvaluationBytes(otherReport, otherBundleID, keys.evPriv, "ev-1", now.Format(time.RFC3339))
	pub20, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n20", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: defaultLayers,
		OMSSignatureBytes: rawOMS, CycloneDXBOMBytes: rawBOM, EvaluationAttestation: otherEvalAtt, EvaluationReportBytes: otherReport, ReleaseQualification: rawRelAtt,
	})
	assertArtifactDenied("N-20", "n20-eval-wrong-bundle", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub20.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonEvaluationGateFailed)

	// N-21: Evaluation suite digest mismatch
	var badSuiteRepMap map[string]any
	_ = json.Unmarshal(rawReport, &badSuiteRepMap)
	badSuiteRepMap["suite"] = map[string]any{
		"name":    "wrong-suite",
		"version": "1.0.0",
		"digest":  "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	}
	rawBadSuiteRep, _ := json.MarshalIndent(badSuiteRepMap, "", "  ")
	badSuiteAtt, _ := verify.SignNativeEvaluationBytes(rawBadSuiteRep, bm.BundleID, keys.evPriv, "ev-1", now.Format(time.RFC3339))
	pub21, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n21", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: defaultLayers,
		OMSSignatureBytes: rawOMS, CycloneDXBOMBytes: rawBOM, EvaluationAttestation: badSuiteAtt, EvaluationReportBytes: rawBadSuiteRep, ReleaseQualification: rawRelAtt,
	})
	assertArtifactDenied("N-21", "n21-eval-bad-suite", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub21.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonEvaluationGateFailed)

	// N-22: Stale evaluation timestamp (> 24h maxEvidenceAgeHours)
	staleTime := now.Add(-72 * time.Hour)
	staleReport := buildValidReportBytes(bm.BundleID, staleTime)
	staleEvalAtt, _ := verify.SignNativeEvaluationBytes(staleReport, bm.BundleID, keys.evPriv, "ev-1", staleTime.Format(time.RFC3339))
	pub22, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n22", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: defaultLayers,
		OMSSignatureBytes: rawOMS, CycloneDXBOMBytes: rawBOM, EvaluationAttestation: staleEvalAtt, EvaluationReportBytes: staleReport, ReleaseQualification: rawRelAtt,
	})
	assertArtifactDenied("N-22", "n22-stale-eval", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub22.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonEvaluationGateFailed)

	// N-23: Release qualification cross-evidence mismatch (mismatched bom.digest)
	badCrossRelAtt, _, _ := verify.SignReleaseQualificationBytes(verify.ReleaseBuildInput{
		Timestamp:           now.Format(time.RFC3339),
		Manifest:            bm,
		BOMCanonicalDigest:  "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		BOMSpecVersion:      "1.7",
		OMS:                 omsRes,
		Evaluation:          evalRes,
		RuntimeImage:        testRuntimeImage,
		ServingConfigDigest: testServingCfgHash,
	}, keys.raPriv, "ra-1")
	pub23, _ := regClient.PublishModelBundleAndEvidence(ctx, mockReg.Host, registry.ModelBundleArtifactInput{
		Repository: "models/n23", BundleID: bm.BundleID, BundleManifestBytes: rawManifest, LayerFiles: defaultLayers,
		OMSSignatureBytes: rawOMS, CycloneDXBOMBytes: rawBOM, EvaluationAttestation: rawEvalAtt, EvaluationReportBytes: rawReport, ReleaseQualification: badCrossRelAtt,
	})
	assertArtifactDenied("N-23", "n23-cross-evidence", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: pub23.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: bm.BundleID},
	}, v1alpha1.ReasonCrossEvidenceMismatch)

	// N-24: Workload container with mutable runtime image tag (:latest)
	dec24 := validator.EvaluateWorkload(ctx, "Pod", testNamespace, "n24-pod", "system:serviceaccount:ai-serving:default",
		map[string]string{
			v1alpha1.AnnotationModelArtifact:       "gemma-matrix-base",
			v1alpha1.AnnotationServingConfigDigest: testServingCfgHash,
		},
		nil,
		corev1.PodSpec{Containers: []corev1.Container{{Name: "vllm", Image: "us-west4-docker.pkg.dev/example-project/models/vllm-cpu:latest"}}},
	)
	if dec24.Allowed || dec24.Reason != v1alpha1.ReasonMutableRuntimeImage {
		t.Fatalf("[N-24] expected %s, got %+v", v1alpha1.ReasonMutableRuntimeImage, dec24)
	}

	// N-25: Workload container with digest-pinned image that does not match attested runtime_image
	dec25 := validator.EvaluateWorkload(ctx, "Pod", testNamespace, "n25-pod", "system:serviceaccount:ai-serving:default",
		map[string]string{
			v1alpha1.AnnotationModelArtifact:       "gemma-matrix-base",
			v1alpha1.AnnotationServingConfigDigest: testServingCfgHash,
		},
		nil,
		corev1.PodSpec{Containers: []corev1.Container{{Name: "vllm", Image: "us-west4-docker.pkg.dev/example-project/models/vllm-cpu@sha256:7777777777777777777777777777777777777777777777777777777777777777"}}},
	)
	if dec25.Allowed || dec25.Reason != v1alpha1.ReasonRuntimeImageMismatch {
		t.Fatalf("[N-25] expected %s, got %+v", v1alpha1.ReasonRuntimeImageMismatch, dec25)
	}

	// N-26: Workload with mismatched serving config digest
	dec26 := validator.EvaluateWorkload(ctx, "Pod", testNamespace, "n26-pod", "system:serviceaccount:ai-serving:default",
		map[string]string{
			v1alpha1.AnnotationModelArtifact:       "gemma-matrix-base",
			v1alpha1.AnnotationServingConfigDigest: "sha256:8888888888888888888888888888888888888888888888888888888888888888",
		},
		nil, validPodSpec,
	)
	if dec26.Allowed || dec26.Reason != v1alpha1.ReasonServingConfigDigestMismatch {
		t.Fatalf("[N-26] expected %s, got %+v", v1alpha1.ReasonServingConfigDigestMismatch, dec26)
	}

	// N-27: LoRA adapter bound to wrong base model bundle_id
	loraBadBase := publishFullBundleToRegistry(t, regClient, mockReg.Host, "models/n27-lora", keys, now, verify.BuildManifestOptions{
		PrimaryFormat:        "safetensors",
		HasLoRAAdapter:       true,
		DeclaredBaseModel:    "gemma-matrix-base",
		VerifiedBaseBundleID: "sha256:1212121212121212121212121212121212121212121212121212121212121212",
		ModelName:            "n27-lora",
	})
	assertArtifactDenied("N-27", "n27-lora-bad-base", v1alpha1.ModelArtifactSpec{
		Artifact: v1alpha1.ArtifactSourceSpec{URI: loraBadBase.Published.ModelArtifactURI},
		Identity: v1alpha1.BundleIdentitySpec{BundleID: loraBadBase.BundleID},
		Runtime: v1alpha1.RuntimeBindingSpec{
			RuntimeImage:        testRuntimeImage,
			ServingConfigDigest: testServingCfgHash,
			BaseModelArtifact:   "gemma-matrix-base",
		},
	}, v1alpha1.ReasonLoRABaseModelUnverified)

	// N-28: Expired verification TTL
	validator.NowFunc = func() time.Time { return now.Add(3 * time.Hour) }
	dec28 := validator.EvaluateWorkload(ctx, "Pod", testNamespace, "n28-pod", "system:serviceaccount:ai-serving:default",
		map[string]string{
			v1alpha1.AnnotationModelArtifact:       "gemma-matrix-base",
			v1alpha1.AnnotationServingConfigDigest: testServingCfgHash,
		},
		nil, validPodSpec,
	)
	if dec28.Allowed || dec28.Reason != v1alpha1.ReasonVerificationExpired {
		t.Fatalf("[N-28] expected %s, got %+v", v1alpha1.ReasonVerificationExpired, dec28)
	}
	validator.NowFunc = func() time.Time { return now }

	// Exemption Non-Self-Assignability Check:
	// Even if a workload creator sets BOTH pod.metadata.labels["trust.gke.dev/exempt"]="true" AND
	// pod.spec.serviceAccountName="coredns" in YAML, Handle() evaluates req.UserInfo.Username
	// (the API-server-authenticated requester), so an unauthorized creator cannot self-assign an exemption.
	spoofedPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      "spoofed-exempt-pod",
			Labels:    map[string]string{"trust.gke.dev/exempt": "true"},
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "coredns",
			Containers:         []corev1.Container{{Name: "vllm", Image: testRuntimeImage}},
		},
	}
	rawSpoofed, _ := json.Marshal(spoofedPod)
	spoofResp := validator.Handle(ctx, admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       "uid-spoof-exempt",
			Kind:      metav1.GroupVersionKind{Kind: "Pod"},
			Namespace: testNamespace,
			Name:      "spoofed-exempt-pod",
			Operation: admissionv1.Create,
			UserInfo: authenticationv1.UserInfo{
				Username: "developer@example.com",
			},
			Object: runtime.RawExtension{Raw: rawSpoofed},
		},
	})
	if spoofResp.Allowed {
		t.Fatalf("expected unauthorized creator self-assigning exempt ServiceAccount and label to be DENIED, got ALLOWED")
	}
}
