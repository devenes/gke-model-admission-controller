package registry_test

import (
	"testing"

	"github.com/devenes/gke-model-admission-controller/internal/registry"
)

func TestParseDigestPinnedURI(t *testing.T) {
	validDigest := "sha256:1111111111111111111111111111111111111111111111111111111111111111"

	ref, err := registry.ParseDigestPinnedURI("us-west4-docker.pkg.dev/example-project/models/gemma-2b-it@" + validDigest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Registry != "us-west4-docker.pkg.dev" || ref.Repository != "example-project/models/gemma-2b-it" || ref.Digest != validDigest {
		t.Fatalf("unexpected parsed reference: %+v", ref)
	}

	// Reject mutable tag without digest
	if _, err := registry.ParseDigestPinnedURI("us-west4-docker.pkg.dev/example-project/models/gemma-2b-it:v1"); err == nil {
		t.Fatal("expected mutable tag URI to fail")
	}

	// Reject tag+digest ambiguity
	if _, err := registry.ParseDigestPinnedURI("us-west4-docker.pkg.dev/example-project/models/gemma-2b-it:v1@" + validDigest); err == nil {
		t.Fatal("expected combined tag+digest URI to fail")
	}

	if !registry.IsRegistryAllowed(ref, []string{"us-west4-docker.pkg.dev/example-project/models/"}) {
		t.Fatal("expected registry to be allowed")
	}
	if registry.IsRegistryAllowed(ref, []string{"us-central1-docker.pkg.dev/other-proj/models/"}) {
		t.Fatal("expected untrusted registry to be rejected")
	}
}
