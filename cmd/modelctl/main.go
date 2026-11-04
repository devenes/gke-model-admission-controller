// Package main implements modelctl, a helper CLI for packaging and publishing OCI v1.1 Model Bundles
// and their 4 Referrer artifacts (OMS v1.0, CycloneDX 1.7 AI/ML-BOM, Evaluation Evidence, Release Qualification)
// to Google Artifact Registry or any OCI v1.1 registry.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/devenes/ai-model-provenance-gate/pkg/verify"
	"github.com/devenes/gke-model-admission-controller/api/v1alpha1"
	"github.com/devenes/gke-model-admission-controller/internal/registry"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	sigyaml "sigs.k8s.io/yaml"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "push" {
		args = args[1:]
	}

	fs := flag.NewFlagSet("modelctl", flag.ExitOnError)
	var (
		registryHost        string
		repository          string
		modelName           string
		runtimeImage        string
		servingCfg          string
		scheme              string
		outPolicyPath       string
		outModelPath        string
		namespace           string
		pushBinPath         string
		pushBinTag          string
		modelDir            string
		bundleManifestPath  string
		omsSignaturePath    string
		bomPath             string
		evalAttestationPath string
		evalReportPath      string
		releaseQualPath     string
	)
	fs.StringVar(&registryHost, "registry", "us-west4-docker.pkg.dev", "OCI v1.1 registry host")
	fs.StringVar(&repository, "repository", "PROJECT_ID/models/gemma-2b-it", "OCI repository path")
	fs.StringVar(&modelName, "model-name", "gemma-2b-it", "ModelArtifact resource name")
	fs.StringVar(&namespace, "namespace", "ai-serving", "Target Kubernetes namespace for ModelArtifact")
	fs.StringVar(&runtimeImage, "runtime-image", "", "Digest-pinned serving container image (@sha256:...)")
	fs.StringVar(&servingCfg, "serving-config-digest", "sha256:5555555555555555555555555555555555555555555555555555555555555555", "Serving configuration SHA-256 digest")
	fs.StringVar(&scheme, "scheme", "https", "Registry HTTP scheme (https or http)")
	fs.StringVar(&outPolicyPath, "out-policy", "", "Optional path to write generated ClusterModelTrustPolicy YAML")
	fs.StringVar(&outModelPath, "out-model-artifact", "", "Optional path to write generated ModelArtifact YAML")
	fs.StringVar(&pushBinPath, "push-binary-image", "", "If set, package and push a linux/amd64 Go binary as an OCI container image to --registry/--repository")
	fs.StringVar(&pushBinTag, "tag", "v0.1.0", "Optional image tag when --push-binary-image is used")
	fs.StringVar(&modelDir, "model-dir", "", "Optional path to local model bundle directory (if omitted, generates synthetic fixture)")
	fs.StringVar(&bundleManifestPath, "bundle-manifest", "", "Optional path to pre-generated bundle-manifest.json")
	fs.StringVar(&omsSignaturePath, "oms-signature", "", "Optional path to pre-generated OpenSSF Model Signing (OMS v1.0) signature")
	fs.StringVar(&bomPath, "bom", "", "Optional path to pre-generated CycloneDX 1.7 AI/ML-BOM JSON")
	fs.StringVar(&evalAttestationPath, "eval-attestation", "", "Optional path to pre-generated DSSE evaluation attestation")
	fs.StringVar(&evalReportPath, "eval-report", "", "Optional path to pre-generated llm-release-gate report.json")
	fs.StringVar(&releaseQualPath, "release-qualification", "", "Optional path to pre-generated DSSE release qualification attestation")
	_ = fs.Parse(args)

	if pushBinPath != "" {
		client := registry.NewClient(nil)
		client.Scheme = scheme
		tp := registry.NewGCPMetadataTokenProvider()
		tp.GcloudTokenFallback = true
		client.TokenProvider = tp
		uri, dgst, err := client.PublishBinaryContainerImage(context.Background(), registryHost, repository, pushBinTag, pushBinPath, "/etc/ssl/cert.pem")
		if err != nil {
			fatalf("PublishBinaryContainerImage: %v", err)
		}
		outJSON, _ := json.MarshalIndent(map[string]string{
			"imageURI":    uri,
			"imageDigest": dgst,
			"tag":         pushBinTag,
		}, "", "  ")
		fmt.Println(string(outJSON))
		return
	}

	now := time.Now().UTC().Truncate(time.Second)
	msPriv, msPub, msFP, err := verify.GenerateEd25519KeyPairPEM()
	if err != nil {
		fatalf("generate model signer key: %v", err)
	}
	evPriv, evPub, evFP, err := verify.GenerateEd25519KeyPairPEM()
	if err != nil {
		fatalf("generate evaluator key: %v", err)
	}
	raPriv, raPub, raFP, err := verify.GenerateEd25519KeyPairPEM()
	if err != nil {
		fatalf("generate release authority key: %v", err)
	}

	layerPayloads, err := loadOrBuildLayerPayloads(modelDir)
	if err != nil {
		fatalf("load model bundle files: %v", err)
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

	opts := verify.BuildManifestOptions{
		ModelType:    "gemma",
		Architecture: "GemmaForCausalLM",
		ModelName:    modelName,
		Timestamp:    now.Format(time.RFC3339),
	}

	var (
		rawManifest []byte
		bm          verify.BundleManifest
		rawOMS      []byte
		rawBOM      []byte
		bomHash     string
		rawReport   []byte
		rawEvalAtt  []byte
		rawRelAtt   []byte
	)

	if bundleManifestPath != "" {
		rawManifest, err = os.ReadFile(bundleManifestPath)
		if err != nil {
			fatalf("read --bundle-manifest: %v", err)
		}
		if err := json.Unmarshal(rawManifest, &bm); err != nil {
			fatalf("parse --bundle-manifest: %v", err)
		}
	} else {
		rawManifest, bm, err = verify.BuildBundleManifestBytes(descriptors, opts)
		if err != nil {
			fatalf("BuildBundleManifestBytes: %v", err)
		}
	}

	if omsSignaturePath != "" {
		rawOMS, err = os.ReadFile(omsSignaturePath)
		if err != nil {
			fatalf("read --oms-signature: %v", err)
		}
	} else {
		rawOMS, err = verify.SignOMSBytesFromDescriptors(descriptors, msPriv, "ms-1")
		if err != nil {
			fatalf("SignOMSBytesFromDescriptors: %v", err)
		}
	}

	if bomPath != "" {
		rawBOM, err = os.ReadFile(bomPath)
		if err != nil {
			fatalf("read --bom: %v", err)
		}
		bomRes, bErr := verify.VerifyBOMBytes(rawBOM, &bm, descriptors)
		if bErr != nil {
			fatalf("VerifyBOMBytes: %v", bErr)
		}
		bomHash = bomRes.CanonicalHash
	} else {
		rawBOM, bomHash, err = verify.GenerateBOMBytesFromDescriptors(descriptors, opts)
		if err != nil {
			fatalf("GenerateBOMBytesFromDescriptors: %v", err)
		}
	}

	if evalReportPath != "" {
		rawReport, err = os.ReadFile(evalReportPath)
		if err != nil {
			fatalf("read --eval-report: %v", err)
		}
	} else {
		rawReport = buildSampleReport(bm.BundleID, now)
	}

	if evalAttestationPath != "" {
		rawEvalAtt, err = os.ReadFile(evalAttestationPath)
		if err != nil {
			fatalf("read --eval-attestation: %v", err)
		}
	} else {
		rawEvalAtt, err = verify.SignNativeEvaluationBytes(rawReport, bm.BundleID, evPriv, "ev-1", now.Format(time.RFC3339))
		if err != nil {
			fatalf("SignNativeEvaluationBytes: %v", err)
		}
	}

	if releaseQualPath != "" {
		rawRelAtt, err = os.ReadFile(releaseQualPath)
		if err != nil {
			fatalf("read --release-qualification: %v", err)
		}
	} else {
		omsRes, oErr := verify.VerifyOMSBytes(rawOMS, descriptors, verify.OMSVerifyOptions{
			TrustedModelSigners: []verify.TrustedSignerRule{{ID: "ms-1", KeyID: "ms-1", KeyFingerprint: msFP, PublicKeyPEM: msPub}},
		})
		if oErr != nil {
			fatalf("VerifyOMSBytes: %v", oErr)
		}
		evalRes, eErr := verify.VerifyEvaluationBytes(rawReport, rawEvalAtt, verify.EvalVerifyOptions{
			TrustedEvaluators:      []verify.TrustedSignerRule{{ID: "ev-1", KeyID: "ev-1", KeyFingerprint: evFP, PublicKeyPEM: evPub}},
			ExpectedBundleID:       bm.BundleID,
			RequireArtifactBinding: true,
			RequireTrustedEvidence: true,
			AllowPostHocBinding:    false,
			Now:                    now,
		})
		if eErr != nil {
			fatalf("VerifyEvaluationBytes: %v", eErr)
		}
		rawRelAtt, _, err = verify.SignReleaseQualificationBytes(verify.ReleaseBuildInput{
			Timestamp:           now.Format(time.RFC3339),
			Manifest:            bm,
			BOMCanonicalDigest:  bomHash,
			BOMSpecVersion:      "1.7",
			OMS:                 omsRes,
			Evaluation:          evalRes,
			RuntimeImage:        runtimeImage,
			ServingConfigDigest: servingCfg,
		}, raPriv, "ra-1")
		if err != nil {
			fatalf("SignReleaseQualificationBytes: %v", err)
		}
	}

	client := registry.NewClient(nil)
	client.Scheme = scheme
	tp := registry.NewGCPMetadataTokenProvider()
	tp.GcloudTokenFallback = true
	client.TokenProvider = tp

	pub, err := client.PublishModelBundleAndEvidence(context.Background(), registryHost, registry.ModelBundleArtifactInput{
		Repository:            repository,
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
		fatalf("PublishModelBundleAndEvidence: %v", err)
	}

	if outPolicyPath != "" {
		pol := &v1alpha1.ClusterModelTrustPolicy{
			TypeMeta: metav1.TypeMeta{
				APIVersion: v1alpha1.GroupVersion.String(),
				Kind:       "ClusterModelTrustPolicy",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: v1alpha1.DefaultClusterPolicyName,
			},
			Spec: v1alpha1.ClusterModelTrustPolicySpec{
				EnforcementMode:        v1alpha1.EnforcementModeEnforce,
				AllowedRegistries:      []string{registryHost + "/" + strings.Split(repository, "/")[0] + "/"},
				VerificationTTLSeconds: 3600,
				MaxEvidenceAgeHours:    24,
				TrustDomains: v1alpha1.TrustDomainsSpec{
					ModelSigners:       []v1alpha1.TrustedPrincipalSpec{{ID: "model-signer-1", KeyID: "ms-1", KeyFingerprint: msFP, PublicKeyPEM: string(msPub)}},
					Evaluators:         []v1alpha1.TrustedPrincipalSpec{{ID: "eval-signer-1", KeyID: "ev-1", KeyFingerprint: evFP, PublicKeyPEM: string(evPub)}},
					ReleaseAuthorities: []v1alpha1.TrustedPrincipalSpec{{ID: "rel-signer-1", KeyID: "ra-1", KeyFingerprint: raFP, PublicKeyPEM: string(raPub)}},
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
				},
			},
		}
		if rawPol, mErr := sigyaml.Marshal(pol); mErr == nil {
			_ = os.WriteFile(outPolicyPath, rawPol, 0o644)
		}
	}

	if outModelPath != "" {
		art := &v1alpha1.ModelArtifact{
			TypeMeta: metav1.TypeMeta{
				APIVersion: v1alpha1.GroupVersion.String(),
				Kind:       "ModelArtifact",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      modelName,
				Namespace: namespace,
			},
			Spec: v1alpha1.ModelArtifactSpec{
				Artifact: v1alpha1.ArtifactSourceSpec{
					URI: pub.ModelArtifactURI,
				},
				Identity: v1alpha1.BundleIdentitySpec{
					BundleID: bm.BundleID,
				},
				Runtime: v1alpha1.RuntimeBindingSpec{
					RuntimeImage:        runtimeImage,
					ServingConfigDigest: servingCfg,
				},
			},
		}
		if rawArt, mErr := sigyaml.Marshal(art); mErr == nil {
			_ = os.WriteFile(outModelPath, rawArt, 0o644)
		}
	}

	summary := map[string]any{
		"published":                   pub,
		"bundleID":                    bm.BundleID,
		"bundleManifestDigest":        bm.ManifestDigest,
		"bomCanonicalDigest":          bomHash,
		"modelSignerFingerprint":      msFP,
		"modelSignerPublicKeyPEM":     string(msPub),
		"evaluatorFingerprint":        evFP,
		"evaluatorPublicKeyPEM":       string(evPub),
		"releaseAuthorityFingerprint": raFP,
		"releaseAuthorityPublicKey":   string(raPub),
	}
	outJSON, _ := json.MarshalIndent(summary, "", "  ")
	fmt.Println(string(outJSON))
}

func loadOrBuildLayerPayloads(modelDir string) ([]registry.LayerPayload, error) {
	if strings.TrimSpace(modelDir) == "" {
		return []registry.LayerPayload{
			{Path: "config.json", Role: "model_config", Content: []byte(`{"model_type":"gemma","architectures":["GemmaForCausalLM"]}`)},
			{Path: "model.safetensors", Role: "model_weights", Content: []byte("safetensors-weight-layer-payload")},
			{Path: "tokenizer.json", Role: "tokenizer_vocab", Content: []byte(`{"version":"1.0"}`)},
		}, nil
	}
	entries, err := os.ReadDir(modelDir)
	if err != nil {
		return nil, err
	}
	var payloads []registry.LayerPayload
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		content, rErr := os.ReadFile(filepath.Join(modelDir, e.Name()))
		if rErr != nil {
			return nil, rErr
		}
		role := classifyRole(e.Name())
		payloads = append(payloads, registry.LayerPayload{
			Path:    e.Name(),
			Role:    role,
			Content: content,
		})
	}
	sort.Slice(payloads, func(i, j int) bool { return payloads[i].Path < payloads[j].Path })
	return payloads, nil
}

func classifyRole(filename string) string {
	switch {
	case filename == "adapter_config.json":
		return "lora_config"
	case strings.HasPrefix(filename, "adapter_") && strings.HasSuffix(filename, ".safetensors"):
		return "lora_weights"
	case strings.HasSuffix(filename, ".safetensors"):
		return "model_weights"
	case strings.Contains(filename, "tokenizer"):
		return "tokenizer_vocab"
	case strings.HasSuffix(filename, ".json"):
		return "model_config"
	default:
		return "auxiliary_asset"
	}
}

func buildSampleReport(bundleID string, now time.Time) []byte {
	rep := map[string]any{
		"schemaVersion": "llm-release-gate.report/v1",
		"runId":         "run-modelctl-001",
		"timestamp":     now.UTC().Format(time.RFC3339),
		"toolVersion":   "0.1.0",
		"suite": map[string]any{
			"name":    "gke-admission-suite",
			"version": "1.0.0",
			"digest":  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		"dataset": map[string]any{
			"digest":    "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			"caseCount": 1,
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
			"digest":   "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			"metadata": map[string]string{
				"model_bundle_id": bundleID,
			},
		},
		"cases": []map[string]any{
			{"id": "c1", "repetition": 1, "status": "passed", "required": true, "assertions": []any{}},
		},
		"metrics": map[string]any{
			"totalPlannedCases": 1,
			"executedCases":     1,
			"passedCases":       1,
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

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
