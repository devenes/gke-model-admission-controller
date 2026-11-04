package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestKubernetesManifestsAndCRDsAreValidYAML(t *testing.T) {
	k8sPatterns := []string{
		"../config/crd/bases/*.yaml",
		"../examples/*.yaml",
	}

	var files []string
	for _, pattern := range k8sPatterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob %q failed: %v", pattern, err)
		}
		files = append(files, matches...)
	}

	if len(files) == 0 {
		t.Fatal("expected to find CRD and example YAML manifests, found 0")
	}

	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", path, err)
		}

		docs := strings.Split(string(raw), "\n---")
		validDocs := 0
		for _, doc := range docs {
			trimmed := strings.TrimSpace(doc)
			if trimmed == "" || trimmed == "---" {
				continue
			}
			var obj map[string]interface{}
			if err := yaml.Unmarshal([]byte(trimmed), &obj); err != nil {
				t.Fatalf("invalid YAML in %s: %v", path, err)
			}
			if len(obj) == 0 {
				continue
			}
			if _, ok := obj["apiVersion"]; !ok {
				t.Errorf("manifest %s document missing apiVersion", path)
			}
			if _, ok := obj["kind"]; !ok {
				t.Errorf("manifest %s document missing kind", path)
			}
			validDocs++
		}
		if validDocs == 0 {
			t.Errorf("manifest %s contained no Kubernetes YAML documents", path)
		}
	}

	fixtureMatches, err := filepath.Glob("../examples/fixtures/*.yaml")
	if err != nil {
		t.Fatalf("glob fixtures failed: %v", err)
	}
	for _, path := range fixtureMatches {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read fixture %s: %v", path, err)
		}
		var obj map[string]interface{}
		if err := yaml.Unmarshal(raw, &obj); err != nil {
			t.Fatalf("invalid YAML in fixture %s: %v", path, err)
		}
	}
}
