/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"strconv"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	routev1 "github.com/openshift/api/route/v1"
	"github.com/sirupsen/logrus"
	velerov1api "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	velerov2alpha1api "github.com/vmware-tanzu/velero/pkg/apis/velero/v2alpha1"
	uberzap "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	kubevirtv1 "kubevirt.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	oadpv1alpha1 "github.com/migtools/oadp-vm-file-restore/api/v1alpha1"
	"github.com/migtools/oadp-vm-file-restore/internal/common/constant"
	"github.com/migtools/oadp-vm-file-restore/internal/controller"
	"github.com/migtools/oadp-vm-file-restore/internal/velerohelpers"
	// +kubebuilder:scaffold:imports
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(oadpv1alpha1.AddToScheme(scheme))
	utilruntime.Must(velerov1api.AddToScheme(scheme))
	utilruntime.Must(velerov2alpha1api.AddToScheme(scheme))
	utilruntime.Must(kubevirtv1.AddToScheme(scheme))
	utilruntime.Must(routev1.Install(scheme))
	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	logLevel := zapcore.InfoLevel
	// read loglevel string coming from DPA which is a logrus level
	logLevelEnvInvalid := false
	found := false
	var logLevelEnv string
	if logLevelEnv, found = os.LookupEnv(constant.LogLevelEnvVar); found && len(logLevelEnv) > 0 {
		uint64LogLevel, err := strconv.ParseUint(logLevelEnv, constant.Base10, constant.Bits32)
		if err == nil {
			// only change from default if level can be parsed
			level := logrus.Level(uint64LogLevel)
			logLevel, logLevelEnvInvalid = translateLogrusToZapLevel(level)
		} else {
			logLevelEnvInvalid = true
		}
	}

	logFormat := "text"
	if logFormatEnv, found := os.LookupEnv(constant.LogFormatEnvVar); found && len(logFormatEnv) > 0 {
		logFormat = logFormatEnv
	}

	opts := zap.Options{
		Level:       logLevel,
		Development: true,
		Encoder:     encoderForFormat(logFormat),
	}

	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Initialize setup logger after configuring the global logger
	setupLog := ctrl.Log.WithName("setup")

	if logLevelEnvInvalid {
		setupLog.Info(fmt.Sprintf("LogLevelEnv: %v is invalid, using default level: %v", logLevelEnv, logLevel.String()))
	}
	setupLog.Info(fmt.Sprintf("LogLevel: %v", logLevel.String()))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
	}

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	webhookServer := webhook.NewServer(webhookServerOptions)

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	// Get OADP namespace where Velero backups are located
	oadpNamespace := os.Getenv(constant.WatchNamespaceEnvVar)
	if len(oadpNamespace) == 0 {
		setupLog.Error(
			fmt.Errorf("%s environment variable is empty", constant.WatchNamespaceEnvVar),
			"environment variable must be set")
		os.Exit(1)
	}
	setupLog.Info("OADP namespace configured", "namespace", oadpNamespace)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "8f8d9561.openshift.io",
		Logger:                 zap.New(zap.UseFlagOptions(&opts)),
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				// Only watch VMBD and VMFR resources in the OADP namespace
				&oadpv1alpha1.VirtualMachineBackupsDiscovery{}: {
					Namespaces: map[string]cache.Config{
						oadpNamespace: {},
					},
				},
				&oadpv1alpha1.VirtualMachineFileRestore{}: {
					Namespaces: map[string]cache.Config{
						oadpNamespace: {},
					},
				},
				// Other resources (Services, Routes, Pods, Velero Restores, etc.) watch all namespaces
				// to support dynamically created temporary restore namespaces
			},
		},
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Initialize backup contents reader
	backupReader := velerohelpers.NewVeleroBackupContentsReader()
	backupReader.SetClient(mgr.GetClient())

	// Create VirtualMachineBackupsDiscovery controller
	vmbdReconciler := &controller.VirtualMachineBackupsDiscoveryReconciler{
		Client:               mgr.GetClient(),
		Scheme:               mgr.GetScheme(),
		OADPNamespace:        oadpNamespace,
		BackupContentsReader: backupReader,
	}

	if err := vmbdReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VirtualMachineBackupsDiscovery")
		os.Exit(1)
	}

	// Create VirtualMachineFileRestore controller
	vmfrReconciler := &controller.VirtualMachineFileRestoreReconciler{
		Client:               mgr.GetClient(),
		Scheme:               mgr.GetScheme(),
		OADPNamespace:        oadpNamespace,
		BackupContentsReader: backupReader,
	}

	if err := vmfrReconciler.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VirtualMachineFileRestore")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func translateLogrusToZapLevel(level logrus.Level) (logLevel zapcore.Level, logLevelEnvInvalid bool) {
	// only change from default if level can be parsed
	switch level {
	case logrus.DebugLevel, logrus.TraceLevel:
		logLevel = zapcore.DebugLevel
	case logrus.InfoLevel:
		logLevel = zapcore.InfoLevel
	case logrus.WarnLevel:
		logLevel = zapcore.WarnLevel
	case logrus.ErrorLevel:
		logLevel = zapcore.ErrorLevel
	case logrus.FatalLevel:
		logLevel = zapcore.FatalLevel
	case logrus.PanicLevel:
		logLevel = zapcore.PanicLevel
	default:
		logLevelEnvInvalid = true
		logLevel = zapcore.InfoLevel
	}
	return logLevel, logLevelEnvInvalid
}

func encoderForFormat(format string) zapcore.Encoder {
	switch format {
	case "json":
		cfg := uberzap.NewProductionConfig()
		return zapcore.NewJSONEncoder(cfg.EncoderConfig)
	case "text":
		fallthrough
	default:
		cfg := uberzap.NewDevelopmentConfig()
		return zapcore.NewConsoleEncoder(cfg.EncoderConfig)
	}
}
