// Package telemetry registers Prometheus metrics for ModelArtifact reconciliation, OCI registry requests,
// verification cache hits/misses, policy evaluations, and admission decisions.
package telemetry

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	registerOnce sync.Once

	ModelAdmissionRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "model_admission_requests_total",
			Help: "Total number of Kubernetes admission requests received by the GKE Model Admission Controller.",
		},
		[]string{"kind", "operation"},
	)

	ModelAdmissionDecisionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "model_admission_decisions_total",
			Help: "Total number of Kubernetes admission decisions evaluated by decision, reason, kind, and mode.",
		},
		[]string{"decision", "reason", "kind", "mode"},
	)

	ModelAdmissionDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "model_admission_duration_seconds",
			Help:    "Latency of admission webhook validation in seconds.",
			Buckets: []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25},
		},
		[]string{"kind"},
	)

	ModelArtifactVerificationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "model_artifact_verification_total",
			Help: "Total number of ModelArtifact verification reconciliations by result and reason.",
		},
		[]string{"result", "reason"},
	)

	ModelArtifactVerificationDurationSeconds = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "model_artifact_verification_duration_seconds",
			Help:    "Duration of OCI descriptor and supply-chain cryptographic verification in seconds.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
		},
	)

	ModelRegistryRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "model_registry_requests_total",
			Help: "Total number of OCI registry HTTP requests by operation and status.",
		},
		[]string{"operation", "status"},
	)

	ModelRegistryErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "model_registry_errors_total",
			Help: "Total number of OCI registry errors by operation.",
		},
		[]string{"operation"},
	)

	ModelVerificationCacheHitsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "model_verification_cache_hits_total",
			Help: "Total number of admission checks served from verified ModelArtifact and PolicyStore cache.",
		},
	)

	ModelVerificationCacheMissesTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "model_verification_cache_misses_total",
			Help: "Total number of admission checks that encountered unverified, missing, or expired cache entries.",
		},
	)

	ModelPolicyEvaluationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "model_policy_evaluations_total",
			Help: "Total number of ClusterModelTrustPolicy evaluations by policy and result.",
		},
		[]string{"policy", "result"},
	)

	ModelVerificationFailuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "model_verification_failures_total",
			Help: "Total number of model verification failures by stage and reason.",
		},
		[]string{"reason"},
	)

	// Legacy aliases for backwards compatibility
	AdmissionDecisionsTotal     = ModelAdmissionDecisionsTotal
	AdmissionLatencySeconds     = ModelAdmissionDurationSeconds
	ReconcileTotal              = ModelArtifactVerificationTotal
	VerificationDurationSeconds = ModelArtifactVerificationDurationSeconds
)

// Register registers custom controller metrics with the controller-runtime Prometheus registry.
func Register() {
	registerOnce.Do(func() {
		crmetrics.Registry.MustRegister(
			ModelAdmissionRequestsTotal,
			ModelAdmissionDecisionsTotal,
			ModelAdmissionDurationSeconds,
			ModelArtifactVerificationTotal,
			ModelArtifactVerificationDurationSeconds,
			ModelRegistryRequestsTotal,
			ModelRegistryErrorsTotal,
			ModelVerificationCacheHitsTotal,
			ModelVerificationCacheMissesTotal,
			ModelPolicyEvaluationsTotal,
			ModelVerificationFailuresTotal,
		)
	})
}
