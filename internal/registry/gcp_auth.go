package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// GCPMetadataTokenProvider acquires and caches short-lived OAuth2 access tokens from the GKE Workload Identity
// metadata server (`http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token`)
// when talking to Google Artifact Registry (`*.pkg.dev` or `*.gcr.io`).
type GCPMetadataTokenProvider struct {
	mu                  sync.Mutex
	EndpointURL         string
	HTTPClient          *http.Client
	GcloudTokenFallback bool
	cachedToken         string
	expiresAt           time.Time
}

// NewGCPMetadataTokenProvider returns a TokenProvider backed by the GCE/GKE metadata server.
func NewGCPMetadataTokenProvider() *GCPMetadataTokenProvider {
	return &GCPMetadataTokenProvider{
		EndpointURL: "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token",
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
	}
}

func (p *GCPMetadataTokenProvider) BearerToken(ctx context.Context, registryHost string) (string, error) {
	if !strings.HasSuffix(registryHost, ".pkg.dev") && !strings.HasSuffix(registryHost, ".gcr.io") {
		return "", nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cachedToken != "" && time.Now().Add(60*time.Second).Before(p.expiresAt) {
		return p.cachedToken, nil
	}

	if p.HTTPClient == nil {
		p.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	if p.EndpointURL == "" {
		p.EndpointURL = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.EndpointURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		if p.GcloudTokenFallback {
			out, gErr := exec.CommandContext(ctx, "gcloud", "auth", "print-access-token").Output()
			if gErr == nil {
				tok := strings.TrimSpace(string(out))
				if tok != "" {
					p.cachedToken = tok
					p.expiresAt = time.Now().Add(30 * time.Minute)
					return p.cachedToken, nil
				}
			}
		}
		return "", fmt.Errorf("fetch GKE Workload Identity token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("GKE metadata server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tokResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokResp); err != nil {
		return "", fmt.Errorf("decode GKE metadata token response: %w", err)
	}
	if tokResp.AccessToken == "" {
		return "", fmt.Errorf("GKE metadata server returned empty access_token")
	}
	p.cachedToken = tokResp.AccessToken
	ttl := time.Duration(tokResp.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	p.expiresAt = time.Now().Add(ttl)
	return p.cachedToken, nil
}
