// Package controller implements Kubernetes reconcilers for ClusterModelTrustPolicy and ModelArtifact.
package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/devenes/gke-model-admission-controller/api/v1alpha1"
	"github.com/devenes/gke-model-admission-controller/internal/policy"
	"github.com/devenes/gke-model-admission-controller/internal/telemetry"
)

// PolicyStore provides thread-safe in-memory access to resolved ClusterModelTrustPolicy instances,
// with on-demand direct reader fallback so admission webhooks never suffer stale cache bypasses.
type PolicyStore struct {
	mu       sync.RWMutex
	policies map[string]*policy.ResolvedPolicy
}

// NewPolicyStore creates an initialized PolicyStore.
func NewPolicyStore() *PolicyStore {
	return &PolicyStore{
		policies: make(map[string]*policy.ResolvedPolicy),
	}
}

// Set stores a resolved policy by name.
func (s *PolicyStore) Set(name string, resolved *policy.ResolvedPolicy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if resolved == nil {
		delete(s.policies, name)
		return
	}
	s.policies[name] = resolved
}

// Get retrieves a resolved policy by name.
func (s *PolicyStore) Get(name string) *policy.ResolvedPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.policies[name]
}

// ClusterModelTrustPolicyReconciler reconciles ClusterModelTrustPolicy objects.
type ClusterModelTrustPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Store  *PolicyStore
}

func (r *ClusterModelTrustPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var pol v1alpha1.ClusterModelTrustPolicy
	if err := r.Get(ctx, req.NamespacedName, &pol); err != nil {
		if apierrors.IsNotFound(err) {
			if r.Store != nil {
				r.Store.Set(req.Name, nil)
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	now := metav1.NewTime(time.Now().UTC())
	resolved, err := policy.Resolve(ctx, &pol, r.fetchSecretKey)
	if err != nil {
		telemetry.ModelPolicyEvaluationsTotal.WithLabelValues(pol.Name, "error").Inc()
		logger.Error(err, "failed to resolve ClusterModelTrustPolicy", "policy", pol.Name)
		if r.Store != nil {
			r.Store.Set(pol.Name, nil)
		}
		pol.Status.ObservedGeneration = pol.Generation
		pol.Status.Ready = false
		pol.Status.PolicyDigest = ""
		pol.Status.FailureReason = v1alpha1.ReasonClusterPolicyNotReady
		pol.Status.FailureMessage = err.Error()
		pol.Status.LastReconciledAt = &now
		pol.Status.Conditions = []metav1.Condition{
			{
				Type:               v1alpha1.ConditionReady,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: pol.Generation,
				LastTransitionTime: now,
				Reason:             v1alpha1.ReasonClusterPolicyNotReady,
				Message:            err.Error(),
			},
		}
		_ = r.Status().Update(ctx, &pol)
		return ctrl.Result{}, nil
	}

	if r.Store != nil {
		r.Store.Set(pol.Name, resolved)
	}
	telemetry.ModelPolicyEvaluationsTotal.WithLabelValues(pol.Name, "ready").Inc()

	pol.Status.ObservedGeneration = pol.Generation
	pol.Status.Ready = true
	pol.Status.PolicyDigest = resolved.PolicyDigest
	pol.Status.ResolvedModelSignerFingerprints = resolved.ModelSignerFPs
	pol.Status.ResolvedEvaluatorFingerprints = resolved.EvaluatorFPs
	pol.Status.ResolvedReleaseAuthorityFingerprints = resolved.ReleaseAuthorityFPs
	pol.Status.LastReconciledAt = &now
	pol.Status.FailureReason = ""
	pol.Status.FailureMessage = ""
	pol.Status.Conditions = []metav1.Condition{
		{
			Type:               v1alpha1.ConditionReady,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: pol.Generation,
			LastTransitionTime: now,
			Reason:             v1alpha1.ReasonVerified,
			Message:            fmt.Sprintf("resolved policyDigest %s", resolved.PolicyDigest),
		},
	}

	if err := r.Status().Update(ctx, &pol); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *ClusterModelTrustPolicyReconciler) fetchSecretKey(ctx context.Context, namespace, name, key string) ([]byte, error) {
	var sec corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &sec); err != nil {
		return nil, err
	}
	val, ok := sec.Data[key]
	if !ok || len(val) == 0 {
		return nil, fmt.Errorf("secret %s/%s does not contain non-empty key %q", namespace, name, key)
	}
	return val, nil
}

// SetupWithManager registers ClusterModelTrustPolicyReconciler with the controller-runtime Manager.
func (r *ClusterModelTrustPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ClusterModelTrustPolicy{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				var list v1alpha1.ClusterModelTrustPolicyList
				if err := r.List(ctx, &list); err != nil {
					return nil
				}
				var reqs []reconcile.Request
				for _, item := range list.Items {
					reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Name: item.Name}})
				}
				return reqs
			}),
		).
		Complete(r)
}
