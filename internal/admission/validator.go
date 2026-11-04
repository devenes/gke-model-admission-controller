// Package admission implements the ValidatingAdmissionWebhook for Pod, Deployment,
// StatefulSet, DaemonSet, Job, and CronJob resources without performing network or registry I/O.
package admission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/devenes/gke-model-admission-controller/api/v1alpha1"
	"github.com/devenes/gke-model-admission-controller/internal/controller"
	"github.com/devenes/gke-model-admission-controller/internal/policy"
	"github.com/devenes/gke-model-admission-controller/internal/registry"
	"github.com/devenes/gke-model-admission-controller/internal/telemetry"
)

// Decision holds the structured outcome of an admission validation check.
type Decision struct {
	Allowed         bool
	Reason          string
	Message         string
	Warnings        []string
	EnforcementMode string
}

// WorkloadValidator validates Kubernetes workloads and Pods against verified ModelArtifact status and ClusterModelTrustPolicy.
type WorkloadValidator struct {
	Client      client.Reader
	APIReader   client.Reader
	PolicyStore *controller.PolicyStore
	NowFunc     func() time.Time
}

// NewWorkloadValidator creates a new WorkloadValidator.
func NewWorkloadValidator(cachedReader client.Reader, apiReader client.Reader, store *controller.PolicyStore) *WorkloadValidator {
	return &WorkloadValidator{
		Client:      cachedReader,
		APIReader:   apiReader,
		PolicyStore: store,
		NowFunc:     func() time.Time { return time.Now().UTC() },
	}
}

func (v *WorkloadValidator) reader() client.Reader {
	if v.APIReader != nil {
		return v.APIReader
	}
	return v.Client
}

func (v *WorkloadValidator) now() time.Time {
	if v.NowFunc != nil {
		return v.NowFunc().UTC()
	}
	return time.Now().UTC()
}

type extractedWorkload struct {
	Kind               string
	Namespace          string
	Name               string
	ServiceAccountName string
	Annotations        map[string]string
	Labels             map[string]string
	PodSpec            corev1.PodSpec
}

// Handle implements admission.Handler for controller-runtime webhook server.
func (v *WorkloadValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	start := time.Now()
	kind := req.Kind.Kind
	op := string(req.Operation)
	if op == "" {
		op = "CREATE"
	}
	telemetry.ModelAdmissionRequestsTotal.WithLabelValues(kind, op).Inc()
	defer func() {
		telemetry.AdmissionLatencySeconds.WithLabelValues(kind).Observe(time.Since(start).Seconds())
	}()

	wl, err := extractWorkloadFromRaw(kind, req.Namespace, req.Name, req.Object.Raw)
	if err != nil {
		telemetry.AdmissionDecisionsTotal.WithLabelValues("deny", "InvalidWorkloadObject", kind, v1alpha1.EnforcementModeEnforce).Inc()
		return admission.Denied(fmt.Sprintf("[InvalidWorkloadObject] failed to decode %s: %v", kind, err))
	}

	if wl.ServiceAccountName != "" && strings.ContainsAny(wl.ServiceAccountName, ":/") {
		telemetry.AdmissionDecisionsTotal.WithLabelValues("deny", "InvalidWorkloadObject", kind, v1alpha1.EnforcementModeEnforce).Inc()
		return admission.Denied(fmt.Sprintf("[InvalidWorkloadObject] invalid serviceAccountName %q in %s", wl.ServiceAccountName, kind))
	}

	// Security invariant: Exemption checks evaluate only the API-server-authenticated requester
	// identity (req.UserInfo.Username), NEVER the user-supplied podSpec.serviceAccountName field,
	// preventing any workload creator from self-assigning an exempt ServiceAccount in workload YAML.
	requesterIdentity := strings.TrimSpace(req.UserInfo.Username)

	dec := v.EvaluateWorkload(ctx, wl.Kind, wl.Namespace, wl.Name, requesterIdentity, wl.Annotations, wl.Labels, wl.PodSpec)
	if dec.Allowed {
		decisionLabel := "allow"
		if len(dec.Warnings) > 0 {
			decisionLabel = "audit_warn"
		}
		telemetry.AdmissionDecisionsTotal.WithLabelValues(decisionLabel, dec.Reason, kind, dec.EnforcementMode).Inc()
		resp := admission.Allowed(dec.Message)
		if len(dec.Warnings) > 0 {
			resp = resp.WithWarnings(dec.Warnings...)
		}
		return resp
	}

	telemetry.AdmissionDecisionsTotal.WithLabelValues("deny", dec.Reason, kind, dec.EnforcementMode).Inc()
	resp := admission.Denied(fmt.Sprintf("[%s] %s", dec.Reason, dec.Message))
	if resp.Result != nil {
		resp.Result.Reason = metav1.StatusReason(dec.Reason)
		resp.Result.Code = http.StatusForbidden
	}
	return resp
}

// ServeHTTP allows WorkloadValidator to be mounted directly on a standard net/http mux (for standalone or TLS webhook servers).
func (v *WorkloadValidator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var review admissionv1.AdmissionReview
	if err := json.NewDecoder(r.Body).Decode(&review); err != nil {
		http.Error(w, fmt.Sprintf("decode AdmissionReview: %v", err), http.StatusBadRequest)
		return
	}
	if review.Request == nil {
		http.Error(w, "empty AdmissionReview.Request", http.StatusBadRequest)
		return
	}

	resp := v.Handle(r.Context(), admission.Request{AdmissionRequest: *review.Request})
	resp.UID = review.Request.UID

	outReview := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: admissionv1.SchemeGroupVersion.String(),
			Kind:       "AdmissionReview",
		},
		Response: &resp.AdmissionResponse,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(outReview)
}

func extractWorkloadFromRaw(kind, fallbackNS, fallbackName string, raw []byte) (extractedWorkload, error) {
	switch kind {
	case "Pod":
		var pod corev1.Pod
		if err := json.Unmarshal(raw, &pod); err != nil {
			return extractedWorkload{}, err
		}
		return buildExtracted("Pod", fallbackNS, fallbackName, pod.ObjectMeta, pod.ObjectMeta, pod.Spec), nil

	case "Deployment":
		var dep appsv1.Deployment
		if err := json.Unmarshal(raw, &dep); err != nil {
			return extractedWorkload{}, err
		}
		return buildExtracted("Deployment", fallbackNS, fallbackName, dep.ObjectMeta, dep.Spec.Template.ObjectMeta, dep.Spec.Template.Spec), nil

	case "StatefulSet":
		var sts appsv1.StatefulSet
		if err := json.Unmarshal(raw, &sts); err != nil {
			return extractedWorkload{}, err
		}
		return buildExtracted("StatefulSet", fallbackNS, fallbackName, sts.ObjectMeta, sts.Spec.Template.ObjectMeta, sts.Spec.Template.Spec), nil

	case "DaemonSet":
		var ds appsv1.DaemonSet
		if err := json.Unmarshal(raw, &ds); err != nil {
			return extractedWorkload{}, err
		}
		return buildExtracted("DaemonSet", fallbackNS, fallbackName, ds.ObjectMeta, ds.Spec.Template.ObjectMeta, ds.Spec.Template.Spec), nil

	case "Job":
		var job batchv1.Job
		if err := json.Unmarshal(raw, &job); err != nil {
			return extractedWorkload{}, err
		}
		return buildExtracted("Job", fallbackNS, fallbackName, job.ObjectMeta, job.Spec.Template.ObjectMeta, job.Spec.Template.Spec), nil

	case "CronJob":
		var cj batchv1.CronJob
		if err := json.Unmarshal(raw, &cj); err != nil {
			return extractedWorkload{}, err
		}
		mergedTop := cj.ObjectMeta
		if mergedTop.Annotations == nil {
			mergedTop.Annotations = map[string]string{}
		}
		for k, val := range cj.Spec.JobTemplate.Annotations {
			if _, exists := mergedTop.Annotations[k]; !exists {
				mergedTop.Annotations[k] = val
			}
		}
		return buildExtracted("CronJob", fallbackNS, fallbackName, mergedTop, cj.Spec.JobTemplate.Spec.Template.ObjectMeta, cj.Spec.JobTemplate.Spec.Template.Spec), nil

	default:
		return extractedWorkload{}, fmt.Errorf("unsupported workload kind %q", kind)
	}
}

func buildExtracted(kind, fallbackNS, fallbackName string, topMeta, podMeta metav1.ObjectMeta, podSpec corev1.PodSpec) extractedWorkload {
	ns := topMeta.Namespace
	if ns == "" {
		ns = fallbackNS
	}
	name := topMeta.Name
	if name == "" {
		name = fallbackName
	}
	anns := make(map[string]string)
	for k, v := range topMeta.Annotations {
		anns[k] = v
	}
	// Pod template annotations take precedence so child Pods carry the exact same binding
	for k, v := range podMeta.Annotations {
		anns[k] = v
	}
	lbls := make(map[string]string)
	for k, v := range topMeta.Labels {
		lbls[k] = v
	}
	for k, v := range podMeta.Labels {
		lbls[k] = v
	}
	sa := podSpec.ServiceAccountName
	if sa == "" {
		sa = "default"
	}
	return extractedWorkload{
		Kind:               kind,
		Namespace:          ns,
		Name:               name,
		ServiceAccountName: sa,
		Annotations:        anns,
		Labels:             lbls,
		PodSpec:            podSpec,
	}
}

// EvaluateWorkload performs the deterministic, zero-network-I/O admission check for a Pod or controller workload.
func (v *WorkloadValidator) EvaluateWorkload(
	ctx context.Context,
	kind string,
	namespace string,
	name string,
	serviceAccountUsername string,
	annotations map[string]string,
	labels map[string]string,
	podSpec corev1.PodSpec,
) Decision {
	// 1. Resolve active ClusterModelTrustPolicy cleanly (fail-closed if policy cannot be loaded).
	activePol, err := v.getFreshPolicy(ctx, v1alpha1.DefaultClusterPolicyName)
	if err != nil {
		return Decision{
			Allowed:         false,
			Reason:          v1alpha1.ReasonClusterPolicyNotReady,
			Message:         fmt.Sprintf("cannot evaluate admission because ClusterModelTrustPolicy %q is not ready: %v", v1alpha1.DefaultClusterPolicyName, err),
			EnforcementMode: v1alpha1.EnforcementModeEnforce,
		}
	}

	// 2. Check explicit policy exemptions (ServiceAccount or Pod labels, which are covered by policyDigest).
	if activePol.IsExempt(serviceAccountUsername, labels) {
		return Decision{
			Allowed:         true,
			Reason:          "ExemptWorkload",
			Message:         fmt.Sprintf("%s %s/%s is exempt under policy %s", kind, namespace, name, activePol.PolicyDigest),
			EnforcementMode: activePol.EnforcementMode,
		}
	}

	// 3. Enforce Default-Deny ModelArtifact annotation in enforced namespaces.
	artifactName := strings.TrimSpace(annotations[v1alpha1.AnnotationModelArtifact])
	if artifactName == "" {
		return v.denyOrAudit(
			activePol,
			v1alpha1.ReasonMissingModelArtifactReference,
			fmt.Sprintf("%s %s/%s in enforced namespace %q must declare annotation %q referencing a verified ModelArtifact", kind, namespace, name, namespace, v1alpha1.AnnotationModelArtifact),
		)
	}

	// 4. Fetch and validate the primary ModelArtifact.
	var artifact v1alpha1.ModelArtifact
	if err := v.reader().Get(ctx, types.NamespacedName{Namespace: namespace, Name: artifactName}, &artifact); err != nil {
		if apierrors.IsNotFound(err) {
			return v.denyOrAudit(
				activePol,
				v1alpha1.ReasonModelArtifactNotFound,
				fmt.Sprintf("referenced ModelArtifact %q does not exist in namespace %q", artifactName, namespace),
			)
		}
		return v.denyOrAudit(
			activePol,
			v1alpha1.ReasonVerificationPending,
			fmt.Sprintf("failed to read ModelArtifact %s/%s: %v", namespace, artifactName, err),
		)
	}

	// If the ModelArtifact selects a custom ClusterModelTrustPolicy, resolve that policy.
	if customPolName := strings.TrimSpace(artifact.Spec.PolicyName); customPolName != "" && customPolName != activePol.Name {
		customPol, cErr := v.getFreshPolicy(ctx, customPolName)
		if cErr != nil {
			return Decision{
				Allowed:         false,
				Reason:          v1alpha1.ReasonClusterPolicyNotReady,
				Message:         fmt.Sprintf("ClusterModelTrustPolicy %q referenced by ModelArtifact %s/%s is not ready: %v", customPolName, namespace, artifactName, cErr),
				EnforcementMode: v1alpha1.EnforcementModeEnforce,
			}
		}
		activePol = customPol
	}

	if dec, ok := v.checkArtifactVerifiedState(&artifact, activePol); !ok {
		return dec
	}

	// 5. If the primary ModelArtifact is a LoRA adapter (or if an explicit base/adapter annotation is present),
	// verify the base-to-adapter cryptographic binding.
	specBaseName := strings.TrimSpace(artifact.Spec.Runtime.BaseModelArtifact)
	annBaseName := strings.TrimSpace(annotations[v1alpha1.AnnotationBaseModelArtifact])
	if specBaseName != "" && annBaseName != "" && specBaseName != annBaseName {
		return v.denyOrAudit(
			activePol,
			v1alpha1.ReasonLoRABaseModelUnverified,
			fmt.Sprintf("workload annotation %s=%q conflicts with ModelArtifact %s/%s spec.runtime.baseModelArtifact=%q",
				v1alpha1.AnnotationBaseModelArtifact, annBaseName, namespace, artifactName, specBaseName),
		)
	}
	baseName := specBaseName
	if baseName == "" {
		baseName = annBaseName
	}
	if baseName != "" {
		var baseArtifact v1alpha1.ModelArtifact
		if err := v.reader().Get(ctx, types.NamespacedName{Namespace: namespace, Name: baseName}, &baseArtifact); err != nil {
			return v.denyOrAudit(
				activePol,
				v1alpha1.ReasonLoRABaseModelUnverified,
				fmt.Sprintf("LoRA adapter %s/%s references baseModelArtifact %q which was not found: %v", namespace, artifactName, baseName, err),
			)
		}
		if dec, ok := v.checkArtifactVerifiedState(&baseArtifact, activePol); !ok {
			return v.denyOrAudit(
				activePol,
				v1alpha1.ReasonLoRABaseModelUnverified,
				fmt.Sprintf("baseModelArtifact %s/%s is not verified: [%s] %s", namespace, baseName, dec.Reason, dec.Message),
			)
		}
		if artifact.Status.VerifiedBaseBundleID == "" || artifact.Status.VerifiedBaseBundleID != baseArtifact.Status.VerifiedBundleID {
			return v.denyOrAudit(
				activePol,
				v1alpha1.ReasonLoRABaseModelUnverified,
				fmt.Sprintf("LoRA adapter %s/%s verifiedBaseBundleID (%q) does not match baseModelArtifact %s/%s bundleID (%q)",
					namespace, artifactName, artifact.Status.VerifiedBaseBundleID, namespace, baseName, baseArtifact.Status.VerifiedBundleID),
			)
		}
	} else if activePol.Requirements.RequireVerifiedLoRABase && artifact.Status.VerifiedBaseBundleID != "" {
		return v.denyOrAudit(
			activePol,
			v1alpha1.ReasonLoRABaseModelUnverified,
			fmt.Sprintf("LoRA adapter %s/%s requires a verified base ModelArtifact reference via spec.runtime.baseModelArtifact or annotation %q",
				namespace, artifactName, v1alpha1.AnnotationBaseModelArtifact),
		)
	}

	if adapterName := strings.TrimSpace(annotations[v1alpha1.AnnotationAdapterArtifact]); adapterName != "" {
		var adapterArtifact v1alpha1.ModelArtifact
		if err := v.reader().Get(ctx, types.NamespacedName{Namespace: namespace, Name: adapterName}, &adapterArtifact); err != nil {
			return v.denyOrAudit(
				activePol,
				v1alpha1.ReasonLoRABaseModelUnverified,
				fmt.Sprintf("referenced adapter ModelArtifact %s/%s not found: %v", namespace, adapterName, err),
			)
		}
		if dec, ok := v.checkArtifactVerifiedState(&adapterArtifact, activePol); !ok {
			return v.denyOrAudit(
				activePol,
				v1alpha1.ReasonLoRABaseModelUnverified,
				fmt.Sprintf("adapter ModelArtifact %s/%s is not verified: [%s] %s", namespace, adapterName, dec.Reason, dec.Message),
			)
		}
		if adapterArtifact.Status.VerifiedBaseBundleID != artifact.Status.VerifiedBundleID {
			return v.denyOrAudit(
				activePol,
				v1alpha1.ReasonLoRABaseModelUnverified,
				fmt.Sprintf("adapter ModelArtifact %s/%s verifiedBaseBundleID (%q) does not match base ModelArtifact %s/%s bundleID (%q)",
					namespace, adapterName, adapterArtifact.Status.VerifiedBaseBundleID, namespace, artifactName, artifact.Status.VerifiedBundleID),
			)
		}
	}

	// 6. Enforce Deployment Composition Binding (Immutable Container Images & Attested Runtime Image).
	allContainers := make([]corev1.Container, 0, len(podSpec.InitContainers)+len(podSpec.Containers))
	allContainers = append(allContainers, podSpec.InitContainers...)
	allContainers = append(allContainers, podSpec.Containers...)
	if len(allContainers) == 0 {
		return v.denyOrAudit(activePol, v1alpha1.ReasonRuntimeImageMismatch, "workload PodSpec contains zero containers")
	}

	if activePol.Requirements.RequireImmutableRuntimeImage {
		for _, c := range allContainers {
			if _, err := registry.ParseDigestPinnedURI(c.Image); err != nil {
				return v.denyOrAudit(
					activePol,
					v1alpha1.ReasonMutableRuntimeImage,
					fmt.Sprintf("container %q uses mutable or unpinned image %q (must use @sha256:<64-hex>): %v", c.Name, c.Image, err),
				)
			}
		}
	}

	expectedRuntimeImg := strings.TrimSpace(artifact.Status.RuntimeImage)
	if expectedRuntimeImg == "" {
		expectedRuntimeImg = strings.TrimSpace(artifact.Spec.Runtime.RuntimeImage)
	}
	if expectedRuntimeImg != "" {
		matchedRuntimeImage := false
		for _, c := range podSpec.Containers {
			if strings.TrimSpace(c.Image) == expectedRuntimeImg {
				matchedRuntimeImage = true
				break
			}
		}
		if !matchedRuntimeImage {
			return v.denyOrAudit(
				activePol,
				v1alpha1.ReasonRuntimeImageMismatch,
				fmt.Sprintf("none of the workload containers match the attested runtime image %q for ModelArtifact %s/%s", expectedRuntimeImg, namespace, artifactName),
			)
		}
	}

	// 7. Enforce ServingConfigDigest Binding (if required by policy or attested on ModelArtifact).
	expectedServingCfg := strings.TrimSpace(artifact.Status.ServingConfigDigest)
	if expectedServingCfg == "" {
		expectedServingCfg = strings.TrimSpace(artifact.Spec.Runtime.ServingConfigDigest)
	}
	if activePol.Requirements.RequireServingConfigBinding || expectedServingCfg != "" {
		workloadServingCfg := strings.TrimSpace(annotations[v1alpha1.AnnotationServingConfigDigest])
		if workloadServingCfg == "" {
			for _, c := range podSpec.Containers {
				for _, env := range c.Env {
					if env.Name == "SERVING_CONFIG_DIGEST" && strings.TrimSpace(env.Value) != "" {
						workloadServingCfg = strings.TrimSpace(env.Value)
						break
					}
				}
			}
		}
		if workloadServingCfg == "" {
			return v.denyOrAudit(
				activePol,
				v1alpha1.ReasonServingConfigDigestMismatch,
				fmt.Sprintf("workload %s/%s is missing required serving config digest (annotation %q or env SERVING_CONFIG_DIGEST), expected %q",
					namespace, name, v1alpha1.AnnotationServingConfigDigest, expectedServingCfg),
			)
		}
		if expectedServingCfg != "" && workloadServingCfg != expectedServingCfg {
			return v.denyOrAudit(
				activePol,
				v1alpha1.ReasonServingConfigDigestMismatch,
				fmt.Sprintf("workload serving config digest %q does not match attested digest %q for ModelArtifact %s/%s",
					workloadServingCfg, expectedServingCfg, namespace, artifactName),
			)
		}
	}

	return Decision{
		Allowed:         true,
		Reason:          v1alpha1.ReasonVerified,
		Message:         fmt.Sprintf("admitted %s %s/%s bound to verified ModelArtifact %q (bundleID=%s, policyDigest=%s)", kind, namespace, name, artifactName, artifact.Status.VerifiedBundleID, activePol.PolicyDigest),
		EnforcementMode: activePol.EnforcementMode,
	}
}

func (v *WorkloadValidator) checkArtifactVerifiedState(artifact *v1alpha1.ModelArtifact, activePol *policy.ResolvedPolicy) (Decision, bool) {
	if artifact.Status.ObservedGeneration == 0 && !artifact.Status.Verified && artifact.Status.FailureReason == "" {
		telemetry.ModelVerificationCacheMissesTotal.Inc()
		return v.denyOrAudit(
			activePol,
			v1alpha1.ReasonVerificationPending,
			fmt.Sprintf("ModelArtifact %s/%s has not been reconciled yet", artifact.Namespace, artifact.Name),
		), false
	}
	if artifact.Status.ObservedGeneration != artifact.Generation {
		telemetry.ModelVerificationCacheMissesTotal.Inc()
		return v.denyOrAudit(
			activePol,
			v1alpha1.ReasonStaleGeneration,
			fmt.Sprintf("ModelArtifact %s/%s status.observedGeneration (%d) != metadata.generation (%d)",
				artifact.Namespace, artifact.Name, artifact.Status.ObservedGeneration, artifact.Generation),
		), false
	}
	if !artifact.Status.Verified {
		telemetry.ModelVerificationCacheMissesTotal.Inc()
		reason := artifact.Status.FailureReason
		if reason == "" {
			reason = v1alpha1.ReasonVerificationPending
		}
		msg := artifact.Status.FailureMessage
		if msg == "" {
			msg = fmt.Sprintf("ModelArtifact %s/%s is not verified", artifact.Namespace, artifact.Name)
		}
		return v.denyOrAudit(activePol, reason, msg), false
	}
	if artifact.Status.PolicyDigest != activePol.PolicyDigest {
		telemetry.ModelVerificationCacheMissesTotal.Inc()
		return v.denyOrAudit(
			activePol,
			v1alpha1.ReasonPolicyDigestMismatch,
			fmt.Sprintf("ModelArtifact %s/%s was verified against policyDigest %q, which does not match active ClusterModelTrustPolicy digest %q",
				artifact.Namespace, artifact.Name, artifact.Status.PolicyDigest, activePol.PolicyDigest),
		), false
	}
	now := v.now()
	if artifact.Status.ValidUntil == nil || !now.Before(artifact.Status.ValidUntil.Time) {
		telemetry.ModelVerificationCacheMissesTotal.Inc()
		expStr := "<nil>"
		if artifact.Status.ValidUntil != nil {
			expStr = artifact.Status.ValidUntil.Time.UTC().Format(time.RFC3339)
		}
		return v.denyOrAudit(
			activePol,
			v1alpha1.ReasonVerificationExpired,
			fmt.Sprintf("ModelArtifact %s/%s verification expired at %s (now=%s)",
				artifact.Namespace, artifact.Name, expStr, now.Format(time.RFC3339)),
		), false
	}
	if artifact.Status.VerifiedBundleID != artifact.Spec.Identity.BundleID {
		telemetry.ModelVerificationCacheMissesTotal.Inc()
		return v.denyOrAudit(
			activePol,
			v1alpha1.ReasonBundleIdentityMismatch,
			fmt.Sprintf("ModelArtifact %s/%s status.verifiedBundleID (%q) != spec.identity.bundleID (%q)",
				artifact.Namespace, artifact.Name, artifact.Status.VerifiedBundleID, artifact.Spec.Identity.BundleID),
		), false
	}
	telemetry.ModelVerificationCacheHitsTotal.Inc()
	return Decision{Allowed: true}, true
}

func (v *WorkloadValidator) denyOrAudit(pol *policy.ResolvedPolicy, reason string, msg string) Decision {
	mode := v1alpha1.EnforcementModeEnforce
	if pol != nil && pol.EnforcementMode != "" {
		mode = pol.EnforcementMode
	}
	if mode == v1alpha1.EnforcementModeAudit {
		return Decision{
			Allowed:         true,
			Reason:          reason,
			Message:         fmt.Sprintf("audit mode allowed violation [%s]: %s", reason, msg),
			Warnings:        []string{fmt.Sprintf("ClusterModelTrustPolicy Audit Violation [%s]: %s", reason, msg)},
			EnforcementMode: v1alpha1.EnforcementModeAudit,
		}
	}
	return Decision{
		Allowed:         false,
		Reason:          reason,
		Message:         msg,
		EnforcementMode: v1alpha1.EnforcementModeEnforce,
	}
}

func (v *WorkloadValidator) getFreshPolicy(ctx context.Context, polName string) (*policy.ResolvedPolicy, error) {
	var pol v1alpha1.ClusterModelTrustPolicy
	if err := v.reader().Get(ctx, types.NamespacedName{Name: polName}, &pol); err != nil {
		return nil, err
	}
	// If PolicyStore already holds the resolved policy for the exact same generation and policyDigest, check if Secrets could have changed by re-resolving via reader.
	resolved, err := policy.Resolve(ctx, &pol, func(ctx context.Context, ns, name, key string) ([]byte, error) {
		var sec corev1.Secret
		if err := v.reader().Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &sec); err != nil {
			return nil, err
		}
		val, ok := sec.Data[key]
		if !ok || len(val) == 0 {
			return nil, fmt.Errorf("secret %s/%s missing key %q", ns, name, key)
		}
		return val, nil
	})
	if err != nil {
		return nil, err
	}
	if v.PolicyStore != nil {
		v.PolicyStore.Set(polName, resolved)
	}
	return resolved, nil
}
