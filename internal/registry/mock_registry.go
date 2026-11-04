package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// InMemoryRegistry implements an OCI Distribution v1.1 and Referrers API HTTP server for deterministic tests.
// It records per-blob download counts so tests can prove zero weight-blob downloads occurred.
type InMemoryRegistry struct {
	mu           sync.RWMutex
	Server       *httptest.Server
	Host         string
	manifests    map[string][]byte       // key: repo + "@" + digest
	blobs        map[string][]byte       // key: repo + "@" + digest
	referrers    map[string][]Descriptor // key: repo + "@" + subjectDigest
	blobGetCount map[string]int          // key: digest
}

// NewInMemoryRegistry starts a new httptest OCI v1.1 registry server.
func NewInMemoryRegistry() *InMemoryRegistry {
	reg := &InMemoryRegistry{
		manifests:    make(map[string][]byte),
		blobs:        make(map[string][]byte),
		referrers:    make(map[string][]Descriptor),
		blobGetCount: make(map[string]int),
	}
	reg.Server = httptest.NewServer(http.HandlerFunc(reg.serveHTTP))
	reg.Host = strings.TrimPrefix(reg.Server.URL, "http://")
	return reg
}

// Close shuts down the httptest server.
func (r *InMemoryRegistry) Close() {
	if r.Server != nil {
		r.Server.Close()
	}
}

// BlobDownloadCount returns how many times a given blob digest was fetched via GET /v2/<repo>/blobs/<digest>.
func (r *InMemoryRegistry) BlobDownloadCount(digest string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.blobGetCount[digest]
}

// CorruptBlob overwrites a stored blob's bytes without changing its key (to test controller digest verification).
func (r *InMemoryRegistry) CorruptBlob(repository, digest string, corrupted []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blobs[repository+"@"+digest] = corrupted
}

func (r *InMemoryRegistry) serveHTTP(w http.ResponseWriter, req *http.Request) {
	path := req.URL.Path
	if path == "/v2/" || path == "/v2" {
		w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
		w.WriteHeader(http.StatusOK)
		return
	}
	if !strings.HasPrefix(path, "/v2/") {
		http.NotFound(w, req)
		return
	}
	rest := strings.TrimPrefix(path, "/v2/")

	// Match /v2/<repo>/blobs/uploads/...
	if idx := strings.LastIndex(rest, "/blobs/uploads/"); idx > 0 {
		repo := rest[:idx]
		switch req.Method {
		case http.MethodPost:
			w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/session-1", repo))
			w.WriteHeader(http.StatusAccepted)
			return
		case http.MethodPut:
			body, err := io.ReadAll(req.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			dgst := req.URL.Query().Get("digest")
			actual := computeSHA256Prefixed(body)
			if dgst != "" && dgst != actual {
				http.Error(w, fmt.Sprintf("digest mismatch: got %s, want %s", actual, dgst), http.StatusBadRequest)
				return
			}
			r.mu.Lock()
			r.blobs[repo+"@"+actual] = body
			r.mu.Unlock()
			w.Header().Set("Docker-Content-Digest", actual)
			w.WriteHeader(http.StatusCreated)
			return
		}
	}

	// Match /v2/<repo>/blobs/<digest>
	if idx := strings.LastIndex(rest, "/blobs/"); idx > 0 {
		repo := rest[:idx]
		dgst := rest[idx+len("/blobs/"):]
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.mu.Lock()
		raw, ok := r.blobs[repo+"@"+dgst]
		if ok && req.Method == http.MethodGet {
			r.blobGetCount[dgst]++
		}
		r.mu.Unlock()
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Docker-Content-Digest", dgst)
		w.WriteHeader(http.StatusOK)
		if req.Method == http.MethodGet {
			_, _ = w.Write(raw)
		}
		return
	}

	// Match /v2/<repo>/manifests/<ref>
	if idx := strings.LastIndex(rest, "/manifests/"); idx > 0 {
		repo := rest[:idx]
		ref := rest[idx+len("/manifests/"):]
		switch req.Method {
		case http.MethodPut:
			body, err := io.ReadAll(req.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			actual := computeSHA256Prefixed(body)
			var m Manifest
			if err := json.Unmarshal(body, &m); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			r.mu.Lock()
			r.manifests[repo+"@"+actual] = body
			if ref != actual {
				r.manifests[repo+"@"+ref] = body
			}
			if m.Subject != nil && m.Subject.Digest != "" {
				subjKey := repo + "@" + m.Subject.Digest
				artType := m.ArtifactType
				if artType == "" {
					artType = m.Config.MediaType
				}
				desc := Descriptor{
					MediaType:    OCIManifestSchemaV2,
					ArtifactType: artType,
					Digest:       actual,
					Size:         int64(len(body)),
					Annotations:  m.Annotations,
				}
				r.referrers[subjKey] = append(r.referrers[subjKey], desc)
			}
			r.mu.Unlock()
			w.Header().Set("Docker-Content-Digest", actual)
			w.WriteHeader(http.StatusCreated)
			return

		case http.MethodGet, http.MethodHead:
			r.mu.RLock()
			raw, ok := r.manifests[repo+"@"+ref]
			r.mu.RUnlock()
			if !ok {
				http.NotFound(w, req)
				return
			}
			w.Header().Set("Content-Type", OCIManifestSchemaV2)
			w.Header().Set("Docker-Content-Digest", computeSHA256Prefixed(raw))
			w.WriteHeader(http.StatusOK)
			if req.Method == http.MethodGet {
				_, _ = w.Write(raw)
			}
			return
		}
	}

	// Match /v2/<repo>/referrers/<digest>
	if idx := strings.LastIndex(rest, "/referrers/"); idx > 0 {
		repo := rest[:idx]
		subjDigest := rest[idx+len("/referrers/"):]
		filterType := req.URL.Query().Get("artifactType")

		r.mu.RLock()
		all := r.referrers[repo+"@"+subjDigest]
		r.mu.RUnlock()

		var filtered []Descriptor
		for _, d := range all {
			if filterType != "" && !MatchesArtifactType(d.ArtifactType, filterType) {
				continue
			}
			filtered = append(filtered, d)
		}
		if filtered == nil {
			filtered = []Descriptor{}
		}
		idxDoc := Index{
			SchemaVersion: 2,
			MediaType:     OCIImageIndexMediaType,
			Manifests:     filtered,
		}
		raw, _ := json.MarshalIndent(idxDoc, "", "  ")
		w.Header().Set("Content-Type", OCIImageIndexMediaType)
		if filterType != "" {
			w.Header().Set("OCI-Filters-Applied", "artifactType")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
		return
	}

	http.NotFound(w, req)
}
