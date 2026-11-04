package controller

import (
	"context"
	"fmt"
	"strings"
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
	"github.com/devenes/gke-model-admission-controller/internal/verification"
)

// ModelArtifactReconciler reconciles ModelArtifact resources against OCI v1.1 registries and ClusterModelTrustPolicy.
type ModelArtifactReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	PolicyStore *PolicyStore
	Engine      *verification.Engine
}

func (r *ModelArtifactReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	start := time.Now()

	var artifact v1alpha1.ModelArtifact
	if err := r.Get(ctx, req.NamespacedName, &artifact); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	polName := strings.TrimSpace(artifact.Spec.PolicyName)
	if polName == "" {
		polName = v1alpha1.DefaultClusterPolicyName
	}

	resolvedPol, err := r.resolveActivePolicy(ctx, polName)
	if err != nil {
		logger.Info("ClusterModelTrustPolicy not ready yet", "policy", polName, "error", err.Error())
		now := metav1.NewTime(time.Now().UTC())
		artifact.Status = v1alpha1.ModelArtifactStatus{
			ObservedGeneration: artifact.Generation,
			Verified:           false,
			PolicyName:         polName,
			FailureReason:      v1alpha1.ReasonClusterPolicyNotReady,
			FailureMessage:     err.Error(),
			Conditions: []metav1.Condition{
				{
					Type:               v1alpha1.ConditionReady,
					Status:             metav1.ConditionFalse,
					ObservedGeneration: artifact.Generation,
					LastTransitionTime: now,
					Reason:             v1alpha1.ReasonClusterPolicyNotReady,
					Message:            err.Error(),
				},
			},
		}
		telemetry.ReconcileTotal.WithLabelValues("failed", v1alpha1.ReasonClusterPolicyNotReady).Inc()
		telemetry.ModelVerificationFailuresTotal.WithLabelValues(v1alpha1.ReasonClusterPolicyNotReady).Inc()
		_ = r.Status().Update(ctx, &artifact)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	// If Spec.Runtime.BaseModelArtifact is specified (LoRA adapter), resolve and verify the base ModelArtifact first.
	var expectedBaseBundleID string
	baseName := strings.TrimSpace(artifact.Spec.Runtime.BaseModelArtifact)
	if baseName != "" {
		var baseArtifact v1alpha1.ModelArtifact
		if err := r.Get(ctx, types.NamespacedName{Namespace: artifact.Namespace, Name: baseName}, &baseArtifact); err != nil {
			now := metav1.NewTime(time.Now().UTC())
			msg := fmt.Sprintf("referenced baseModelArtifact %s/%s not found: %v", artifact.Namespace, baseName, err)
			artifact.Status = v1alpha1.ModelArtifactStatus{
				ObservedGeneration: artifact.Generation,
				Verified:           false,
				PolicyName:         resolvedPol.Name,
				PolicyDigest:       resolvedPol.PolicyDigest,
				FailureReason:      v1alpha1.ReasonLoRABaseModelUnverified,
				FailureMessage:     msg,
				Conditions: []metav1.Condition{
					{
						Type:               v1alpha1.ConditionReady,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: artifact.Generation,
						LastTransitionTime: now,
						Reason:             v1alpha1.ReasonLoRABaseModelUnverified,
						Message:            msg,
					},
				},
			}
			telemetry.ReconcileTotal.WithLabelValues("failed", v1alpha1.ReasonLoRABaseModelUnverified).Inc()
			telemetry.ModelVerificationFailuresTotal.WithLabelValues(v1alpha1.ReasonLoRABaseModelUnverified).Inc()
			_ = r.Status().Update(ctx, &artifact)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		if !baseArtifact.Status.Verified ||
			baseArtifact.Status.ObservedGeneration != baseArtifact.Generation ||
			baseArtifact.Status.PolicyDigest != resolvedPol.PolicyDigest ||
			baseArtifact.Status.VerifiedBundleID == "" {
			now := metav1.NewTime(time.Now().UTC())
			msg := fmt.Sprintf("referenced baseModelArtifact %s/%s is not currently verified under active policy %s", artifact.Namespace, baseName, resolvedPol.PolicyDigest)
			artifact.Status = v1alpha1.ModelArtifactStatus{
				ObservedGeneration: artifact.Generation,
				Verified:           false,
				PolicyName:         resolvedPol.Name,
				PolicyDigest:       resolvedPol.PolicyDigest,
				FailureReason:      v1alpha1.ReasonLoRABaseModelUnverified,
				FailureMessage:     msg,
				Conditions: []metav1.Condition{
					{
						Type:               v1alpha1.ConditionReady,
						Status:             metav1.ConditionFalse,
						ObservedGeneration: artifact.Generation,
						LastTransitionTime: now,
						Reason:             v1alpha1.ReasonLoRABaseModelUnverified,
						Message:            msg,
					},
				},
			}
			telemetry.ReconcileTotal.WithLabelValues("failed", v1alpha1.ReasonLoRABaseModelUnverified).Inc()
			telemetry.ModelVerificationFailuresTotal.WithLabelValues(v1alpha1.ReasonLoRABaseModelUnverified).Inc()
			_ = r.Status().Update(ctx, &artifact)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		expectedBaseBundleID = baseArtifact.Status.VerifiedBundleID
	}

	newStatus := r.Engine.VerifyModelArtifact(ctx, &artifact, resolvedPol, verification.VerifyOptions{
		ExpectedBaseBundleID: expectedBaseBundleID,
	})
	telemetry.VerificationDurationSeconds.Observe(time.Since(start).Seconds())

	if newStatus.Verified {
		telemetry.ReconcileTotal.WithLabelValues("verified", v1alpha1.ReasonVerified).Inc()
	} else {
		telemetry.ReconcileTotal.WithLabelValues("failed", newStatus.FailureReason).Inc()
		telemetry.ModelVerificationFailuresTotal.WithLabelValues(newStatus.FailureReason).Inc()
	}

	artifact.Status = newStatus
	if err := r.Status().Update(ctx, &artifact); err != nil {
		return ctrl.Result{}, err
	}

	if newStatus.Verified && newStatus.ValidUntil != nil {
		requeueIn := time.Until(newStatus.ValidUntil.Time) - (time.Duration(resolvedPol.VerificationTTL.Seconds()*0.1) * time.Second)
		if requeueIn < 30*time.Second {
			requeueIn = 30 * time.Second
		}
		return ctrl.Result{RequeueAfter: requeueIn}, nil
	}
	return ctrl.Result{}, nil
}

func (r *ModelArtifactReconciler) resolveActivePolicy(ctx context.Context, polName string) (*policy.ResolvedPolicy, error) {
	if r.PolicyStore != nil {
		if cached := r.PolicyStore.Get(polName); cached != nil {
			return cached, nil
		}
	}
	var pol v1alpha1.ClusterModelTrustPolicy
	if err := r.Get(ctx, types.NamespacedName{Name: polName}, &pol); err != nil {
		return nil, fmt.Errorf("fetch ClusterModelTrustPolicy %q: %w", polName, err)
	}
	resolved, err := policy.Resolve(ctx, &pol, func(ctx context.Context, ns, name, key string) ([]byte, error) {
		var sec corev1.Secret
		if err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &sec); err != nil {
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
	if r.PolicyStore != nil {
		r.PolicyStore.Set(polName, resolved)
	}
	return resolved, nil
}

// SetupWithManager registers ModelArtifactReconciler with the controller-runtime Manager.
func (r *ModelArtifactReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ModelArtifact{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&v1alpha1.ClusterModelTrustPolicy{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				var list v1alpha1.ModelArtifactList
				if err := r.List(ctx, &list); err != nil {
					return nil
				}
				var reqs []reconcile.Request
				for _, item := range list.Items {
					reqs = append(reqs, reconcile.Request{
						NamespacedName: types.NamespacedName{Namespace: item.Namespace, Name: item.Name},
					})
				}
				return reqs
			}),
		).
		Complete(r)
}
