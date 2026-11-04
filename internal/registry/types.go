// Package registry implements an OCI Distribution v1.1 client, Referrers API discovery,
// and an in-memory OCI v1.1 registry server for testing descriptor-only model verification.
package registry

import (
	"fmt"
	"regexp"
	"strings"
)

// Canonical OCI 1.1 media types and artifact types defined in ADR-002.
const (
	OCIManifestSchemaV2     = "application/vnd.oci.image.manifest.v1+json"
	OCIImageIndexMediaType  = "application/vnd.oci.image.index.v1+json"
	OCIEmptyConfigMediaType = "application/vnd.oci.empty.v1+json"
	OCIEmptyConfigDigest    = "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"

	ArtifactTypeModelBundle        = "application/vnd.devenes.model-bundle.v1"
	ArtifactTypeModelBundleAlias   = "application/vnd.devenes.model.bundle.v1+json"
	MediaTypeBundleManifestConfig  = "application/vnd.devenes.model-bundle.manifest.v1+json"
	MediaTypeSafetensorsLayer      = "application/vnd.devenes.model.weights.safetensors.v1"
	MediaTypeSafetensorsIndexLayer = "application/vnd.devenes.model.weights.index.v1+json"
	MediaTypeModelConfigLayer      = "application/vnd.devenes.model.config.v1+json"
	MediaTypeTokenizerLayer        = "application/vnd.devenes.model.tokenizer.v1"
	MediaTypeLoRAAdapterLayer      = "application/vnd.devenes.model.adapter.v1"
	MediaTypeGenericLayer          = "application/octet-stream"

	ArtifactTypeOMSSignature      = "application/vnd.openssf.model-signing.signature.v1+json"
	ArtifactTypeOMSSignatureAlias = "application/vnd.devenes.oms.signature.v1+json"
	MediaTypeDSSELayer            = "application/vnd.dsse.envelope.v1+json"

	ArtifactTypeCycloneDXBOM      = "application/vnd.cyclonedx+json"
	ArtifactTypeCycloneDXBOMAlias = "application/vnd.devenes.cyclonedx.mlbom.v1+json"
	MediaTypeCycloneDXLayer       = "application/vnd.cyclonedx+json; version=1.7"

	ArtifactTypeEvaluationAttestation = "application/vnd.devenes.evaluation.attestation.v1+json"
	MediaTypeEvaluationReportLayer    = "application/vnd.devenes.llm-release-gate.report.v1+json"

	ArtifactTypeReleaseQualification      = "application/vnd.devenes.release-qualification.v1+json"
	ArtifactTypeReleaseQualificationAlias = "application/vnd.devenes.release.qualification.v1+json"

	// OCI Descriptor Annotations per ADR-002.
	AnnotationBundlePath       = "io.devenes.model.path"
	AnnotationBundleRole       = "io.devenes.model.role"
	AnnotationBundleID         = "io.devenes.model.bundle-id"
	AnnotationEvidenceRole     = "io.devenes.evidence.role"
	AnnotationPredicateType    = "io.devenes.attestation.predicate-type"
	AnnotationCreatedTimestamp = "org.opencontainers.image.created"

	// MaxMetadataBlobBytes caps any manifest, config, BOM, or DSSE blob fetched by the controller (16 MiB).
	MaxMetadataBlobBytes int64 = 16 * 1024 * 1024
)

var sha256DigestRegex = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Descriptor represents an OCI Content Descriptor.
type Descriptor struct {
	MediaType    string            `json:"mediaType"`
	ArtifactType string            `json:"artifactType,omitempty"`
	Digest       string            `json:"digest"`
	Size         int64             `json:"size"`
	Annotations  map[string]string `json:"annotations,omitempty"`
}

// Manifest represents an OCI Image Manifest v1.1 (schemaVersion 2).
type Manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType"`
	ArtifactType  string            `json:"artifactType,omitempty"`
	Config        Descriptor        `json:"config"`
	Layers        []Descriptor      `json:"layers"`
	Subject       *Descriptor       `json:"subject,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// Index represents an OCI Image Index v1.1 returned by the Referrers API.
type Index struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType"`
	Manifests     []Descriptor      `json:"manifests"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// ArtifactReference holds a parsed, digest-pinned OCI artifact URI.
type ArtifactReference struct {
	Raw        string
	Registry   string
	Repository string
	Digest     string
}

// ParseDigestPinnedURI parses an immutable OCI artifact reference of the form
// "<registry>/<repository>@sha256:<64-hex>" (with optional oci:// prefix).
// Mutable tags without "@sha256:..." are rejected.
func ParseDigestPinnedURI(uri string) (ArtifactReference, error) {
	trimmed := strings.TrimSpace(uri)
	trimmed = strings.TrimPrefix(trimmed, "oci://")
	if trimmed == "" {
		return ArtifactReference{}, fmt.Errorf("artifact URI cannot be empty")
	}
	atIdx := strings.LastIndex(trimmed, "@")
	if atIdx <= 0 {
		return ArtifactReference{}, fmt.Errorf("artifact URI %q must be pinned by immutable @sha256:<64-hex> digest (mutable tags are forbidden)", uri)
	}
	repoPart := trimmed[:atIdx]
	digestPart := trimmed[atIdx+1:]
	if !sha256DigestRegex.MatchString(digestPart) {
		return ArtifactReference{}, fmt.Errorf("artifact URI %q has invalid digest %q (expected sha256:<64-lowercase-hex>)", uri, digestPart)
	}
	// Reject tag+digest ambiguity if colon appears after last slash in repoPart
	lastSlash := strings.LastIndex(repoPart, "/")
	if lastSlash <= 0 || lastSlash == len(repoPart)-1 {
		return ArtifactReference{}, fmt.Errorf("artifact URI %q must include both registry host and repository path", uri)
	}
	if strings.Contains(repoPart[lastSlash+1:], ":") {
		return ArtifactReference{}, fmt.Errorf("artifact URI %q must not combine a mutable tag with a digest", uri)
	}
	firstSlash := strings.Index(repoPart, "/")
	regHost := repoPart[:firstSlash]
	repository := repoPart[firstSlash+1:]
	if regHost == "" || repository == "" {
		return ArtifactReference{}, fmt.Errorf("artifact URI %q has empty registry or repository", uri)
	}
	return ArtifactReference{
		Raw:        trimmed,
		Registry:   regHost,
		Repository: repository,
		Digest:     digestPart,
	}, nil
}

// IsRegistryAllowed checks whether an ArtifactReference matches at least one allowed registry prefix.
func IsRegistryAllowed(ref ArtifactReference, allowedPrefixes []string) bool {
	if len(allowedPrefixes) == 0 {
		return false
	}
	fullRepo := ref.Registry + "/" + ref.Repository
	for _, prefix := range allowedPrefixes {
		cleanPrefix := strings.TrimPrefix(strings.TrimSpace(prefix), "oci://")
		if cleanPrefix == "" {
			continue
		}
		if fullRepo == cleanPrefix || strings.HasPrefix(fullRepo, strings.TrimSuffix(cleanPrefix, "/")+"/") || ref.Registry == strings.TrimSuffix(cleanPrefix, "/") {
			return true
		}
	}
	return false
}

// MatchesArtifactType checks whether actual matches expected or its canonical vendor alias.
func MatchesArtifactType(actual, expected string) bool {
	if expected == "" || actual == expected {
		return true
	}
	switch expected {
	case ArtifactTypeModelBundle, ArtifactTypeModelBundleAlias:
		return actual == ArtifactTypeModelBundle || actual == ArtifactTypeModelBundleAlias
	case ArtifactTypeOMSSignature, ArtifactTypeOMSSignatureAlias:
		return actual == ArtifactTypeOMSSignature || actual == ArtifactTypeOMSSignatureAlias
	case ArtifactTypeCycloneDXBOM, ArtifactTypeCycloneDXBOMAlias:
		return actual == ArtifactTypeCycloneDXBOM || actual == ArtifactTypeCycloneDXBOMAlias
	case ArtifactTypeReleaseQualification, ArtifactTypeReleaseQualificationAlias:
		return actual == ArtifactTypeReleaseQualification || actual == ArtifactTypeReleaseQualificationAlias
	default:
		return false
	}
}
