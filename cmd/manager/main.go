// Package main runs the GKE Model Admission Controller manager and ValidatingAdmissionWebhook server.
package main

import (
	"flag"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/devenes/gke-model-admission-controller/api/v1alpha1"
	"github.com/devenes/gke-model-admission-controller/internal/admission"
	"github.com/devenes/gke-model-admission-controller/internal/controller"
	"github.com/devenes/gke-model-admission-controller/internal/registry"
	"github.com/devenes/gke-model-admission-controller/internal/telemetry"
	"github.com/devenes/gke-model-admission-controller/internal/verification"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(batchv1.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
	var (
		metricsAddr          string
		probeAddr            string
		webhookPort          int
		webhookCertDir       string
		enableLeaderElection bool
		registryScheme       string
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.IntVar(&webhookPort, "webhook-port", 9443, "The port the validating admission webhook binds to.")
	flag.StringVar(&webhookCertDir, "webhook-cert-dir", "/tmp/k8s-webhook-server/serving-certs", "Directory containing tls.crt and tls.key for the webhook.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", true, "Enable leader election for controller manager.")
	flag.StringVar(&registryScheme, "registry-scheme", "https", "HTTP scheme for OCI registry requests (https or http).")

	opts := zap.Options{Development: false}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	telemetry.Register()

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:    webhookPort,
			CertDir: webhookCertDir,
		}),
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "gke-model-admission-controller.trust.gke.dev",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	policyStore := controller.NewPolicyStore()
	regClient := registry.NewClient(nil)
	regClient.Scheme = registryScheme
	regClient.TokenProvider = registry.NewGCPMetadataTokenProvider()

	verifyEngine := verification.NewEngine(regClient)

	if err := (&controller.ClusterModelTrustPolicyReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Store:  policyStore,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClusterModelTrustPolicy")
		os.Exit(1)
	}

	if err := (&controller.ModelArtifactReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		PolicyStore: policyStore,
		Engine:      verifyEngine,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ModelArtifact")
		os.Exit(1)
	}

	validator := admission.NewWorkloadValidator(mgr.GetClient(), mgr.GetAPIReader(), policyStore)
	mgr.GetWebhookServer().Register("/validate-workloads", &webhook.Admission{Handler: validator})

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting gke-model-admission-controller manager", "webhookPort", webhookPort)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
