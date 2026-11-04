package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/devenes/gke-model-admission-controller/internal/telemetry"
)

// TokenProvider supplies an optional Bearer token for authenticating to OCI registries (e.g., GKE Workload Identity).
type TokenProvider interface {
	BearerToken(ctx context.Context, registryHost string) (string, error)
}

// Client is a descriptor-first OCI Distribution v1.1 and Referrers API client.
// It strictly enforces SHA-256 verification and size limits on all fetched manifests and metadata blobs,
// and never downloads model weight layers.
type Client struct {
	HTTPClient    *http.Client
	Scheme        string // "https" (default) or "http" (for local httptest registries)
	MaxBlobBytes  int64
	TokenProvider TokenProvider
}

// NewClient returns a new OCI v1.1 Client with safe defaults.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{
		HTTPClient:   httpClient,
		Scheme:       "https",
		MaxBlobBytes: MaxMetadataBlobBytes,
	}
}

func (c *Client) schemeForHost(host string) string {
	if c.Scheme != "" {
		return c.Scheme
	}
	if strings.HasPrefix(host, "127.0.0.1:") || strings.HasPrefix(host, "localhost:") {
		return "http"
	}
	return "https"
}

func (c *Client) maxBytes() int64 {
	if c.MaxBlobBytes <= 0 {
		return MaxMetadataBlobBytes
	}
	return c.MaxBlobBytes
}

func computeSHA256Prefixed(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (c *Client) doGetVerified(ctx context.Context, regHost, endpointPath, acceptHeader, expectedDigest string, maxBytes int64) ([]byte, http.Header, error) {
	if maxBytes <= 0 || maxBytes > c.maxBytes() {
		maxBytes = c.maxBytes()
	}
	u := fmt.Sprintf("%s://%s%s", c.schemeForHost(regHost), regHost, endpointPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, err
	}
	if acceptHeader != "" {
		req.Header.Set("Accept", acceptHeader)
	}
	if c.TokenProvider != nil {
		tok, tErr := c.TokenProvider.BearerToken(ctx, regHost)
		if tErr != nil {
			return nil, nil, fmt.Errorf("acquire registry auth token for %s: %w", regHost, tErr)
		}
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("GET %s: %w", u, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodySnippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, resp.Header, fmt.Errorf("GET %s returned HTTP %d: %s", u, resp.StatusCode, strings.TrimSpace(string(bodySnippet)))
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, resp.Header, fmt.Errorf("read response body from %s: %w", u, err)
	}
	if int64(len(raw)) > maxBytes {
		return nil, resp.Header, fmt.Errorf("response from %s exceeds maximum allowed metadata size (%d bytes)", u, maxBytes)
	}

	if expectedDigest != "" {
		actual := computeSHA256Prefixed(raw)
		if actual != expectedDigest {
			return nil, resp.Header, fmt.Errorf("digest mismatch for %s: got %s, expected %s", endpointPath, actual, expectedDigest)
		}
	}
	return raw, resp.Header, nil
}

// FetchManifest fetches an OCI Image Manifest v1.1 by immutable digest and verifies its exact SHA-256.
func (c *Client) FetchManifest(ctx context.Context, ref ArtifactReference) (Manifest, []byte, error) {
	if !sha256DigestRegex.MatchString(ref.Digest) {
		return Manifest{}, nil, fmt.Errorf("manifest reference %q is not pinned by valid sha256 digest", ref.Digest)
	}
	endpoint := fmt.Sprintf("/v2/%s/manifests/%s", ref.Repository, ref.Digest)
	raw, _, err := c.doGetVerified(ctx, ref.Registry, endpoint, OCIManifestSchemaV2, ref.Digest, c.maxBytes())
	if err != nil {
		telemetry.ModelRegistryRequestsTotal.WithLabelValues("fetch_manifest", "error").Inc()
		telemetry.ModelRegistryErrorsTotal.WithLabelValues("fetch_manifest").Inc()
		return Manifest{}, nil, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		telemetry.ModelRegistryRequestsTotal.WithLabelValues("fetch_manifest", "error").Inc()
		telemetry.ModelRegistryErrorsTotal.WithLabelValues("fetch_manifest").Inc()
		return Manifest{}, nil, fmt.Errorf("unmarshal OCI manifest %s: %w", ref.Digest, err)
	}
	if m.SchemaVersion != 2 {
		telemetry.ModelRegistryRequestsTotal.WithLabelValues("fetch_manifest", "error").Inc()
		telemetry.ModelRegistryErrorsTotal.WithLabelValues("fetch_manifest").Inc()
		return Manifest{}, nil, fmt.Errorf("unsupported OCI manifest schemaVersion %d (expected 2)", m.SchemaVersion)
	}
	telemetry.ModelRegistryRequestsTotal.WithLabelValues("fetch_manifest", "success").Inc()
	return m, raw, nil
}

// FetchMetadataBlob fetches a small config or evidence blob (e.g., bundle-manifest.json, OMS DSSE, BOM, evaluation report/DSSE)
// and verifies both its size and SHA-256 digest against desc.
func (c *Client) FetchMetadataBlob(ctx context.Context, ref ArtifactReference, desc Descriptor) ([]byte, error) {
	if !sha256DigestRegex.MatchString(desc.Digest) {
		return nil, fmt.Errorf("blob descriptor has invalid sha256 digest %q", desc.Digest)
	}
	if desc.Size < 0 || desc.Size > c.maxBytes() {
		return nil, fmt.Errorf("blob %s declared size %d exceeds maximum metadata limit %d", desc.Digest, desc.Size, c.maxBytes())
	}
	// Guard against accidentally fetching weight layers
	switch desc.MediaType {
	case MediaTypeSafetensorsLayer, MediaTypeLoRAAdapterLayer:
		return nil, fmt.Errorf("refusing to download model weight layer %s (%s) in controller", desc.Digest, desc.MediaType)
	}
	if role := desc.Annotations[AnnotationBundleRole]; role == "model_weights" || role == "weight_shard" || role == "lora_weights" {
		return nil, fmt.Errorf("refusing to download model weight layer %s (role=%s) in controller", desc.Digest, role)
	}

	endpoint := fmt.Sprintf("/v2/%s/blobs/%s", ref.Repository, desc.Digest)
	raw, _, err := c.doGetVerified(ctx, ref.Registry, endpoint, desc.MediaType, desc.Digest, c.maxBytes())
	if err != nil {
		telemetry.ModelRegistryRequestsTotal.WithLabelValues("fetch_blob", "error").Inc()
		telemetry.ModelRegistryErrorsTotal.WithLabelValues("fetch_blob").Inc()
		return nil, err
	}
	if desc.Size > 0 && int64(len(raw)) != desc.Size {
		telemetry.ModelRegistryRequestsTotal.WithLabelValues("fetch_blob", "error").Inc()
		telemetry.ModelRegistryErrorsTotal.WithLabelValues("fetch_blob").Inc()
		return nil, fmt.Errorf("blob %s byte length %d does not match descriptor size %d", desc.Digest, len(raw), desc.Size)
	}
	telemetry.ModelRegistryRequestsTotal.WithLabelValues("fetch_blob", "success").Inc()
	return raw, nil
}

// ListReferrers queries the OCI Distribution v1.1 Referrers API (`/v2/<repo>/referrers/<digest>?artifactType=...`)
// and filters results client-side as well per OCI v1.1 specification.
// Descriptors are sorted newest-first by org.opencontainers.image.created annotation (with digest tie-breaking).
func (c *Client) ListReferrers(ctx context.Context, ref ArtifactReference, artifactType string) ([]Descriptor, error) {
	if !sha256DigestRegex.MatchString(ref.Digest) {
		return nil, fmt.Errorf("subject digest %q is invalid", ref.Digest)
	}
	endpoint := fmt.Sprintf("/v2/%s/referrers/%s", ref.Repository, ref.Digest)
	if artifactType != "" {
		endpoint += "?artifactType=" + url.QueryEscape(artifactType)
	}
	raw, _, err := c.doGetVerified(ctx, ref.Registry, endpoint, OCIImageIndexMediaType, "", c.maxBytes())
	if err != nil {
		telemetry.ModelRegistryRequestsTotal.WithLabelValues("list_referrers", "error").Inc()
		telemetry.ModelRegistryErrorsTotal.WithLabelValues("list_referrers").Inc()
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(raw, &idx); err != nil {
		telemetry.ModelRegistryRequestsTotal.WithLabelValues("list_referrers", "error").Inc()
		telemetry.ModelRegistryErrorsTotal.WithLabelValues("list_referrers").Inc()
		return nil, fmt.Errorf("unmarshal referrers index: %w", err)
	}
	var matched []Descriptor
	for _, d := range idx.Manifests {
		if !MatchesArtifactType(d.ArtifactType, artifactType) {
			continue
		}
		matched = append(matched, d)
	}
	sort.Slice(matched, func(i, j int) bool {
		ti := matched[i].Annotations[AnnotationCreatedTimestamp]
		tj := matched[j].Annotations[AnnotationCreatedTimestamp]
		if ti != tj {
			return ti > tj
		}
		return matched[i].Digest < matched[j].Digest
	})
	telemetry.ModelRegistryRequestsTotal.WithLabelValues("list_referrers", "success").Inc()
	return matched, nil
}

// ResolvedEvidenceBlobs holds the raw verified bytes and OCI manifest digests of all 4 referrer evidence types.
type ResolvedEvidenceBlobs struct {
	SubjectManifestDigest string
	SubjectManifest       Manifest
	BundleManifestBytes   []byte

	OMSReferrerDigest string
	OMSEnvelopeBytes  []byte

	BOMReferrerDigest string
	BOMBytes          []byte

	EvalReferrerDigest   string
	EvalAttestationBytes []byte
	EvalReportBytes      []byte

	ReleaseReferrerDigest   string
	ReleaseAttestationBytes []byte
}

// ResolveReferrerManifest fetches a referrer manifest by digest, verifies that its subject.digest matches subjectDigest,
// and returns the parsed Manifest.
func (c *Client) ResolveReferrerManifest(ctx context.Context, subjectRef ArtifactReference, referrerDigest string, expectedArtifactType string) (Manifest, error) {
	ref := ArtifactReference{
		Registry:   subjectRef.Registry,
		Repository: subjectRef.Repository,
		Digest:     referrerDigest,
	}
	m, _, err := c.FetchManifest(ctx, ref)
	if err != nil {
		return Manifest{}, err
	}
	if !MatchesArtifactType(m.ArtifactType, expectedArtifactType) {
		return Manifest{}, fmt.Errorf("referrer %s artifactType %q != expected %q", referrerDigest, m.ArtifactType, expectedArtifactType)
	}
	if m.Subject == nil || m.Subject.Digest != subjectRef.Digest {
		subj := "<nil>"
		if m.Subject != nil {
			subj = m.Subject.Digest
		}
		return Manifest{}, fmt.Errorf("referrer %s subject.digest %s does not match model manifest %s", referrerDigest, subj, subjectRef.Digest)
	}
	return m, nil
}

// SelectReferrerDigest chooses either the explicitly pinned digest (if non-empty) or the unique referrer from ListReferrers.
// If multiple unpinned referrers of the same required artifactType exist, it fails closed to prevent ambiguity.
func (c *Client) SelectReferrerDigest(ctx context.Context, subjectRef ArtifactReference, artifactType string, pinnedDigest string) (string, error) {
	if strings.TrimSpace(pinnedDigest) != "" {
		return strings.TrimSpace(pinnedDigest), nil
	}
	refs, err := c.ListReferrers(ctx, subjectRef, artifactType)
	if err != nil {
		return "", err
	}
	if len(refs) == 0 {
		return "", fmt.Errorf("no OCI referrer found with artifactType %q for subject %s", artifactType, subjectRef.Digest)
	}
	if len(refs) > 1 {
		return "", fmt.Errorf("duplicate or ambiguous OCI referrers (%d found) with artifactType %q for subject %s without pinned digest", len(refs), artifactType, subjectRef.Digest)
	}
	return refs[0].Digest, nil
}
