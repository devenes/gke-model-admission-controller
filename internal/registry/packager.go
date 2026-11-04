package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/devenes/ai-model-provenance-gate/pkg/verify"
)

// ModelBundleArtifactInput holds the payloads to publish a complete OCI v1.1 Model Bundle and its 4 Referrer artifacts.
type ModelBundleArtifactInput struct {
	Repository            string
	BundleID              string
	BundleManifestBytes   []byte
	LayerFiles            []LayerPayload
	OMSSignatureBytes     []byte
	CycloneDXBOMBytes     []byte
	EvaluationAttestation []byte
	EvaluationReportBytes []byte
	ReleaseQualification  []byte
	CreatedTimestamp      string
}

// LayerPayload represents a model file layer descriptor and its optional raw bytes (for test or small-model pushes).
type LayerPayload struct {
	Path      string
	Role      string
	MediaType string
	Content   []byte
}

// PublishedArtifactSet records the immutable OCI digests produced by PublishModelBundleAndEvidence.
type PublishedArtifactSet struct {
	ModelArtifactURI           string
	ModelManifestDigest        string
	ConfigBlobDigest           string
	OMSSignatureManifestDigest string
	BOMManifestDigest          string
	EvaluationManifestDigest   string
	ReleaseQualManifestDigest  string
	ObservedDescriptors        []verify.FileEntry
}

// PushBlob uploads a blob via monolithic POST+PUT `/v2/<repo>/blobs/uploads/?digest=...` to an OCI v1.1 registry.
func (c *Client) PushBlob(ctx context.Context, regHost, repository string, content []byte) (Descriptor, error) {
	dgst := computeSHA256Prefixed(content)
	startURL := fmt.Sprintf("%s://%s/v2/%s/blobs/uploads/", c.schemeForHost(regHost), regHost, repository)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, startURL, nil)
	if err != nil {
		return Descriptor{}, err
	}
	if c.TokenProvider != nil {
		if tok, tErr := c.TokenProvider.BearerToken(ctx, regHost); tErr == nil && tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return Descriptor{}, fmt.Errorf("POST %s: %w", startURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return Descriptor{}, fmt.Errorf("POST %s returned %d: %s", startURL, resp.StatusCode, string(body))
	}

	loc := resp.Header.Get("Location")
	if loc == "" {
		return Descriptor{}, fmt.Errorf("missing Location header from blob upload initiation")
	}
	var putURL string
	if strings.HasPrefix(loc, "http://") || strings.HasPrefix(loc, "https://") {
		putURL = loc
	} else {
		putURL = fmt.Sprintf("%s://%s%s", c.schemeForHost(regHost), regHost, loc)
	}
	sep := "?"
	if strings.Contains(putURL, "?") {
		sep = "&"
	}
	putURL = putURL + sep + "digest=" + url.QueryEscape(dgst)

	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL, bytes.NewReader(content))
	if err != nil {
		return Descriptor{}, err
	}
	putReq.Header.Set("Content-Type", "application/octet-stream")
	if c.TokenProvider != nil {
		if tok, tErr := c.TokenProvider.BearerToken(ctx, regHost); tErr == nil && tok != "" {
			putReq.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	putResp, err := c.HTTPClient.Do(putReq)
	if err != nil {
		return Descriptor{}, fmt.Errorf("PUT %s: %w", putURL, err)
	}
	defer func() { _ = putResp.Body.Close() }()
	if putResp.StatusCode != http.StatusCreated && putResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(putResp.Body, 1024))
		return Descriptor{}, fmt.Errorf("PUT %s returned %d: %s", putURL, putResp.StatusCode, string(body))
	}
	return Descriptor{
		Digest: dgst,
		Size:   int64(len(content)),
	}, nil
}

// PushManifest serializes and uploads an OCI Image Manifest v1.1 by its SHA-256 digest.
func (c *Client) PushManifest(ctx context.Context, regHost, repository string, m Manifest) (Descriptor, []byte, error) {
	if m.SchemaVersion == 0 {
		m.SchemaVersion = 2
	}
	if m.MediaType == "" {
		m.MediaType = OCIManifestSchemaV2
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return Descriptor{}, nil, err
	}
	dgst := computeSHA256Prefixed(raw)
	putURL := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", c.schemeForHost(regHost), regHost, repository, dgst)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL, bytes.NewReader(raw))
	if err != nil {
		return Descriptor{}, nil, err
	}
	req.Header.Set("Content-Type", OCIManifestSchemaV2)
	if c.TokenProvider != nil {
		if tok, tErr := c.TokenProvider.BearerToken(ctx, regHost); tErr == nil && tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return Descriptor{}, nil, fmt.Errorf("PUT manifest %s: %w", putURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return Descriptor{}, nil, fmt.Errorf("PUT manifest %s returned %d: %s", putURL, resp.StatusCode, string(body))
	}
	return Descriptor{
		MediaType:    OCIManifestSchemaV2,
		ArtifactType: m.ArtifactType,
		Digest:       dgst,
		Size:         int64(len(raw)),
		Annotations:  m.Annotations,
	}, raw, nil
}

func mediaTypeForRole(role string) string {
	switch role {
	case "model_weights", "weight_shard":
		return MediaTypeSafetensorsLayer
	case "weight_index":
		return MediaTypeSafetensorsIndexLayer
	case "model_config", "generation_config", "quantization_config":
		return MediaTypeModelConfigLayer
	case "tokenizer_config", "tokenizer_vocab", "special_tokens", "chat_template":
		return MediaTypeTokenizerLayer
	case "lora_config", "lora_weights", "lora_index":
		return MediaTypeLoRAAdapterLayer
	default:
		return MediaTypeGenericLayer
	}
}

// PublishModelBundleAndEvidence uploads the OCI Model Bundle subject manifest, config blob, layer blobs,
// and the 4 OCI 1.1 referrer artifacts (OMS signature, CycloneDX BOM, dual-layer Evaluation Evidence, and Release Qualification).
func (c *Client) PublishModelBundleAndEvidence(ctx context.Context, regHost string, in ModelBundleArtifactInput) (PublishedArtifactSet, error) {
	ts := strings.TrimSpace(in.CreatedTimestamp)
	if ts == "" {
		ts = time.Now().UTC().Format(time.RFC3339)
	}

	// Ensure empty OCI config blob exists in the repository for referrers
	emptyConfigBytes := []byte("{}")
	if _, err := c.PushBlob(ctx, regHost, in.Repository, emptyConfigBytes); err != nil {
		return PublishedArtifactSet{}, fmt.Errorf("push empty config blob: %w", err)
	}
	emptyCfgDesc := Descriptor{
		MediaType: OCIEmptyConfigMediaType,
		Digest:    OCIEmptyConfigDigest,
		Size:      2,
	}

	// 1. Push bundle-manifest.json config blob
	cfgBlob, err := c.PushBlob(ctx, regHost, in.Repository, in.BundleManifestBytes)
	if err != nil {
		return PublishedArtifactSet{}, fmt.Errorf("push bundle-manifest.json config blob: %w", err)
	}
	cfgBlob.MediaType = MediaTypeBundleManifestConfig
	cfgBlob.Annotations = map[string]string{
		AnnotationBundlePath: "bundle-manifest.json",
		AnnotationBundleID:   in.BundleID,
	}

	// 2. Push model file layers
	layers := make([]Descriptor, 0, len(in.LayerFiles))
	observed := make([]verify.FileEntry, 0, len(in.LayerFiles))
	for _, lf := range in.LayerFiles {
		role := lf.Role
		if role == "" {
			r, rErr := verify.ClassifyFileRole(lf.Path)
			if rErr != nil {
				return PublishedArtifactSet{}, rErr
			}
			role = r
		}
		mt := lf.MediaType
		if mt == "" {
			mt = mediaTypeForRole(role)
		}
		blobDesc, bErr := c.PushBlob(ctx, regHost, in.Repository, lf.Content)
		if bErr != nil {
			return PublishedArtifactSet{}, fmt.Errorf("push layer %q: %w", lf.Path, bErr)
		}
		blobDesc.MediaType = mt
		blobDesc.Annotations = map[string]string{
			AnnotationBundlePath: lf.Path,
			AnnotationBundleRole: role,
		}
		layers = append(layers, blobDesc)
		observed = append(observed, verify.FileEntry{
			Path:   lf.Path,
			Role:   role,
			SHA256: blobDesc.Digest,
			Size:   blobDesc.Size,
		})
	}

	// 3. Push subject Model Bundle Manifest
	subjDesc, _, err := c.PushManifest(ctx, regHost, in.Repository, Manifest{
		SchemaVersion: 2,
		MediaType:     OCIManifestSchemaV2,
		ArtifactType:  ArtifactTypeModelBundle,
		Config:        cfgBlob,
		Layers:        layers,
		Annotations: map[string]string{
			AnnotationBundleID:         in.BundleID,
			AnnotationCreatedTimestamp: ts,
		},
	})
	if err != nil {
		return PublishedArtifactSet{}, fmt.Errorf("push subject model manifest: %w", err)
	}

	out := PublishedArtifactSet{
		ModelArtifactURI:    fmt.Sprintf("%s/%s@%s", regHost, in.Repository, subjDesc.Digest),
		ModelManifestDigest: subjDesc.Digest,
		ConfigBlobDigest:    cfgBlob.Digest,
		ObservedDescriptors: observed,
	}

	// 4. Push OMS Signature Referrer (if provided)
	if len(in.OMSSignatureBytes) > 0 {
		omsBlob, err := c.PushBlob(ctx, regHost, in.Repository, in.OMSSignatureBytes)
		if err != nil {
			return PublishedArtifactSet{}, err
		}
		omsBlob.MediaType = MediaTypeDSSELayer
		omsBlob.Annotations = map[string]string{
			AnnotationPredicateType: verify.OMSV1PredicateType,
		}
		refDesc, _, err := c.PushManifest(ctx, regHost, in.Repository, Manifest{
			SchemaVersion: 2,
			MediaType:     OCIManifestSchemaV2,
			ArtifactType:  ArtifactTypeOMSSignature,
			Config:        emptyCfgDesc,
			Layers:        []Descriptor{omsBlob},
			Subject:       &subjDesc,
			Annotations: map[string]string{
				AnnotationPredicateType:    verify.OMSV1PredicateType,
				AnnotationCreatedTimestamp: ts,
			},
		})
		if err != nil {
			return PublishedArtifactSet{}, err
		}
		out.OMSSignatureManifestDigest = refDesc.Digest
	}

	// 5. Push CycloneDX 1.7 BOM Referrer (if provided)
	if len(in.CycloneDXBOMBytes) > 0 {
		bomBlob, err := c.PushBlob(ctx, regHost, in.Repository, in.CycloneDXBOMBytes)
		if err != nil {
			return PublishedArtifactSet{}, err
		}
		bomBlob.MediaType = MediaTypeCycloneDXLayer
		refDesc, _, err := c.PushManifest(ctx, regHost, in.Repository, Manifest{
			SchemaVersion: 2,
			MediaType:     OCIManifestSchemaV2,
			ArtifactType:  ArtifactTypeCycloneDXBOM,
			Config:        emptyCfgDesc,
			Layers:        []Descriptor{bomBlob},
			Subject:       &subjDesc,
			Annotations: map[string]string{
				AnnotationCreatedTimestamp: ts,
			},
		})
		if err != nil {
			return PublishedArtifactSet{}, err
		}
		out.BOMManifestDigest = refDesc.Digest
	}

	// 6. Push Dual-Layer Evaluation Attestation + Report Referrer (if provided)
	if len(in.EvaluationAttestation) > 0 {
		attBlob, err := c.PushBlob(ctx, regHost, in.Repository, in.EvaluationAttestation)
		if err != nil {
			return PublishedArtifactSet{}, err
		}
		attBlob.MediaType = MediaTypeDSSELayer
		attBlob.Annotations = map[string]string{
			AnnotationEvidenceRole:  "evaluation-attestation",
			AnnotationPredicateType: verify.EvaluationEvidencePredicateType,
		}
		evalLayers := []Descriptor{attBlob}
		if len(in.EvaluationReportBytes) > 0 {
			repBlob, err := c.PushBlob(ctx, regHost, in.Repository, in.EvaluationReportBytes)
			if err != nil {
				return PublishedArtifactSet{}, err
			}
			repBlob.MediaType = MediaTypeEvaluationReportLayer
			repBlob.Annotations = map[string]string{
				AnnotationEvidenceRole: "evaluation-report",
			}
			evalLayers = append(evalLayers, repBlob)
		}
		refDesc, _, err := c.PushManifest(ctx, regHost, in.Repository, Manifest{
			SchemaVersion: 2,
			MediaType:     OCIManifestSchemaV2,
			ArtifactType:  ArtifactTypeEvaluationAttestation,
			Config:        emptyCfgDesc,
			Layers:        evalLayers,
			Subject:       &subjDesc,
			Annotations: map[string]string{
				AnnotationPredicateType:    verify.EvaluationEvidencePredicateType,
				AnnotationCreatedTimestamp: ts,
			},
		})
		if err != nil {
			return PublishedArtifactSet{}, err
		}
		out.EvaluationManifestDigest = refDesc.Digest
	}

	// 7. Push Release Qualification Referrer (if provided)
	if len(in.ReleaseQualification) > 0 {
		relBlob, err := c.PushBlob(ctx, regHost, in.Repository, in.ReleaseQualification)
		if err != nil {
			return PublishedArtifactSet{}, err
		}
		relBlob.MediaType = MediaTypeDSSELayer
		relBlob.Annotations = map[string]string{
			AnnotationPredicateType: verify.ReleaseQualificationPredicateType,
		}
		refDesc, _, err := c.PushManifest(ctx, regHost, in.Repository, Manifest{
			SchemaVersion: 2,
			MediaType:     OCIManifestSchemaV2,
			ArtifactType:  ArtifactTypeReleaseQualification,
			Config:        emptyCfgDesc,
			Layers:        []Descriptor{relBlob},
			Subject:       &subjDesc,
			Annotations: map[string]string{
				AnnotationPredicateType:    verify.ReleaseQualificationPredicateType,
				AnnotationCreatedTimestamp: ts,
			},
		})
		if err != nil {
			return PublishedArtifactSet{}, err
		}
		out.ReleaseQualManifestDigest = refDesc.Digest
	}

	return out, nil
}

// PublishBinaryContainerImage packages a static linux/amd64 Go binary and root CA bundle into a rootless
// OCI container image (UID/GID 65532:65532) and pushes it to the target OCI registry without requiring a local Docker daemon.
func (c *Client) PublishBinaryContainerImage(ctx context.Context, regHost, repository, tag, binaryPath, caCertPath string) (string, string, error) {
	binBytes, err := os.ReadFile(binaryPath)
	if err != nil {
		return "", "", fmt.Errorf("read binary %s: %w", binaryPath, err)
	}
	var caBytes []byte
	if caCertPath != "" {
		caBytes, _ = os.ReadFile(caCertPath)
	}

	// Build deterministic uncompressed tar
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	fixedTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	if err := tw.WriteHeader(&tar.Header{
		Name:     "manager",
		Mode:     0o755,
		Size:     int64(len(binBytes)),
		ModTime:  fixedTime,
		Uid:      65532,
		Gid:      65532,
		Typeflag: tar.TypeReg,
	}); err != nil {
		return "", "", err
	}
	if _, err := tw.Write(binBytes); err != nil {
		return "", "", err
	}

	if len(caBytes) > 0 {
		for _, dir := range []string{"etc/", "etc/ssl/", "etc/ssl/certs/"} {
			_ = tw.WriteHeader(&tar.Header{
				Name:     dir,
				Mode:     0o755,
				ModTime:  fixedTime,
				Uid:      65532,
				Gid:      65532,
				Typeflag: tar.TypeDir,
			})
		}
		if err := tw.WriteHeader(&tar.Header{
			Name:     "etc/ssl/certs/ca-certificates.crt",
			Mode:     0o644,
			Size:     int64(len(caBytes)),
			ModTime:  fixedTime,
			Uid:      65532,
			Gid:      65532,
			Typeflag: tar.TypeReg,
		}); err != nil {
			return "", "", err
		}
		if _, err := tw.Write(caBytes); err != nil {
			return "", "", err
		}
	}
	if err := tw.Close(); err != nil {
		return "", "", err
	}

	uncompressedTar := tarBuf.Bytes()
	diffID := computeSHA256Prefixed(uncompressedTar)

	var gzBuf bytes.Buffer
	gzw := gzip.NewWriter(&gzBuf)
	if _, err := gzw.Write(uncompressedTar); err != nil {
		return "", "", err
	}
	if err := gzw.Close(); err != nil {
		return "", "", err
	}
	compressedLayer := gzBuf.Bytes()

	layerDesc, err := c.PushBlob(ctx, regHost, repository, compressedLayer)
	if err != nil {
		return "", "", fmt.Errorf("push container layer blob: %w", err)
	}
	layerDesc.MediaType = "application/vnd.oci.image.layer.v1.tar+gzip"

	imgConfig := map[string]any{
		"created":      fixedTime.Format(time.RFC3339),
		"architecture": "amd64",
		"os":           "linux",
		"config": map[string]any{
			"User":       "65532:65532",
			"Entrypoint": []string{"/manager"},
			"WorkingDir": "/",
		},
		"rootfs": map[string]any{
			"type":     "layers",
			"diff_ids": []string{diffID},
		},
	}
	rawConfig, err := json.MarshalIndent(imgConfig, "", "  ")
	if err != nil {
		return "", "", err
	}
	cfgDesc, err := c.PushBlob(ctx, regHost, repository, rawConfig)
	if err != nil {
		return "", "", fmt.Errorf("push container config blob: %w", err)
	}
	cfgDesc.MediaType = "application/vnd.oci.image.config.v1+json"

	manifest := Manifest{
		SchemaVersion: 2,
		MediaType:     OCIManifestSchemaV2,
		Config:        cfgDesc,
		Layers:        []Descriptor{layerDesc},
	}
	mDesc, rawManifest, err := c.PushManifest(ctx, regHost, repository, manifest)
	if err != nil {
		return "", "", fmt.Errorf("push container manifest by digest: %w", err)
	}

	if tag != "" {
		putURL := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", c.schemeForHost(regHost), regHost, repository, tag)
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, putURL, bytes.NewReader(rawManifest))
		if err != nil {
			return "", "", err
		}
		req.Header.Set("Content-Type", OCIManifestSchemaV2)
		if c.TokenProvider != nil {
			if tok, tErr := c.TokenProvider.BearerToken(ctx, regHost); tErr == nil && tok != "" {
				req.Header.Set("Authorization", "Bearer "+tok)
			}
		}
		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return "", "", fmt.Errorf("PUT tagged manifest: %w", err)
		}
		_ = resp.Body.Close()
	}

	uri := fmt.Sprintf("%s/%s@%s", regHost, repository, mDesc.Digest)
	return uri, mDesc.Digest, nil
}
