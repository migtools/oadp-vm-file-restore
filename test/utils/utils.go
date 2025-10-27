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

// Package utils provides testing utilities for e2e tests
package utils

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	certmanagerVersion = "v1.18.2"
	certmanagerURLTmpl = "https://github.com/cert-manager/cert-manager/releases/download/%s/cert-manager.yaml"

	kubevirtVersion         = "v1.1.1"
	kubevirtOperatorURLTmpl = "https://github.com/kubevirt/kubevirt/releases/download/%s/kubevirt-operator.yaml"
	kubevirtCRURLTmpl       = "https://github.com/kubevirt/kubevirt/releases/download/%s/kubevirt-cr.yaml"
	cdiVersion              = "v1.58.1"
	cdiOperatorURLTmpl      = "https://github.com/kubevirt/containerized-data-importer/" +
		"releases/download/%s/cdi-operator.yaml"
	cdiCRURLTmpl = "https://github.com/kubevirt/containerized-data-importer/releases/download/%s/cdi-cr.yaml"

	veleroVersion = "v1.14.1"

	// Velero plugins - using custom builds with required patches
	kubevirtPluginImage  = "quay.io/migi/kubevirt-velero-plugin:latest"
	openshiftPluginImage = "quay.io/migi/openshift-velero-plugin:modified"
	veleroImage          = "velero/velero:v1.14.1"
	awsPluginImage       = "velero/velero-plugin-for-aws:v1.10.1"
	resticHelperImage    = "velero/velero-restic-restore-helper:v1.14.1"

	// MinIO for backup storage in tests
	minioImage = "quay.io/minio/minio:latest"

	defaultKindBinary  = "kind"
	defaultKindCluster = "kind"
)

func warnError(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "warning: %v\n", err)
}

// Run executes the provided command within this context
func Run(cmd *exec.Cmd) (string, error) {
	dir, _ := GetProjectDir()
	cmd.Dir = dir

	if err := os.Chdir(cmd.Dir); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "chdir dir: %q\n", err)
	}

	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	command := strings.Join(cmd.Args, " ")
	_, _ = fmt.Fprintf(os.Stderr, "running: %q\n", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%q failed with error %q: %w", command, string(output), err)
	}

	return string(output), nil
}

// UninstallCertManager uninstalls the cert manager
func UninstallCertManager() {
	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	cmd := exec.Command("kubectl", "delete", "-f", url)
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}

	// Delete leftover leases in kube-system (not cleaned by default)
	kubeSystemLeases := []string{
		"cert-manager-cainjector-leader-election",
		"cert-manager-controller",
	}
	for _, lease := range kubeSystemLeases {
		cmd = exec.Command("kubectl", "delete", "lease", lease,
			"-n", "kube-system", "--ignore-not-found", "--force", "--grace-period=0")
		if _, err := Run(cmd); err != nil {
			warnError(err)
		}
	}
}

// InstallCertManager installs the cert manager bundle.
func InstallCertManager() error {
	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	cmd := exec.Command("kubectl", "apply", "-f", url)
	if _, err := Run(cmd); err != nil {
		return err
	}
	// Wait for cert-manager-webhook to be ready, which can take time if cert-manager
	// was re-installed after uninstalling on a cluster.
	cmd = exec.Command("kubectl", "wait", "deployment.apps/cert-manager-webhook",
		"--for", "condition=Available",
		"--namespace", "cert-manager",
		"--timeout", "5m",
	)

	_, err := Run(cmd)
	return err
}

// IsCertManagerCRDsInstalled checks if any Cert Manager CRDs are installed
// by verifying the existence of key CRDs related to Cert Manager.
func IsCertManagerCRDsInstalled() bool {
	// List of common Cert Manager CRDs
	certManagerCRDs := []string{
		"certificates.cert-manager.io",
		"issuers.cert-manager.io",
		"clusterissuers.cert-manager.io",
		"certificaterequests.cert-manager.io",
		"orders.acme.cert-manager.io",
		"challenges.acme.cert-manager.io",
	}

	// Execute the kubectl command to get all CRDs
	cmd := exec.Command("kubectl", "get", "crds")
	output, err := Run(cmd)
	if err != nil {
		return false
	}

	// Check if any of the Cert Manager CRDs are present
	crdList := GetNonEmptyLines(output)
	for _, crd := range certManagerCRDs {
		for _, line := range crdList {
			if strings.Contains(line, crd) {
				return true
			}
		}
	}

	return false
}

// LoadImageToKindClusterWithName loads a local docker/podman image to the kind cluster
func LoadImageToKindClusterWithName(name string) error {
	cluster := defaultKindCluster
	if v, ok := os.LookupEnv("KIND_CLUSTER"); ok {
		cluster = v
	}
	kindBinary := defaultKindBinary
	if v, ok := os.LookupEnv("KIND"); ok {
		kindBinary = v
	}

	// Determine which container tool is being used
	containerTool := "docker"
	if v, ok := os.LookupEnv("CONTAINER_TOOL"); ok {
		containerTool = v
	} else {
		// Auto-detect: check if docker or podman is available
		if _, err := exec.LookPath("docker"); err == nil {
			containerTool = "docker"
		} else if _, err := exec.LookPath("podman"); err == nil {
			containerTool = "podman"
		}
	}

	// For podman, we need to save the image to a tar and load from archive
	// For docker, we can use the direct docker-image method
	if containerTool == "podman" {
		// Create temporary tar file (sanitize name: replace / and : with -)
		sanitizedName := strings.ReplaceAll(strings.ReplaceAll(name, "/", "-"), ":", "-")
		tarFile := fmt.Sprintf("/tmp/%s.tar", sanitizedName)
		defer func() { _ = os.Remove(tarFile) }()

		// Save image to tar using podman
		saveCmd := exec.Command("podman", "save", "-o", tarFile, name)
		if _, err := Run(saveCmd); err != nil {
			return fmt.Errorf("failed to save image with podman: %w", err)
		}

		// Load tar into kind
		loadCmd := exec.Command(kindBinary, "load", "image-archive", tarFile, "--name", cluster)
		if _, err := Run(loadCmd); err != nil {
			return fmt.Errorf("failed to load image archive into kind: %w", err)
		}
	} else {
		// Docker: use direct docker-image loading
		kindOptions := []string{"load", "docker-image", name, "--name", cluster}
		cmd := exec.Command(kindBinary, kindOptions...)
		if _, err := Run(cmd); err != nil {
			return err
		}
	}

	return nil
}

// GetNonEmptyLines converts given command output string into individual objects
// according to line breakers, and ignores the empty elements in it.
func GetNonEmptyLines(output string) []string {
	var res []string
	elements := strings.Split(output, "\n")
	for _, element := range elements {
		if element != "" {
			res = append(res, element)
		}
	}

	return res
}

// GetProjectDir will return the directory where the project is
func GetProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, fmt.Errorf("failed to get current working directory: %w", err)
	}
	wd = strings.ReplaceAll(wd, "/test/e2e", "")
	return wd, nil
}

// UncommentCode searches for target in the file and remove the comment prefix
// of the target content. The target content may span multiple lines.
func UncommentCode(filename, target, prefix string) error {
	// false positive
	// nolint:gosec
	content, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read file %q: %w", filename, err)
	}
	strContent := string(content)

	idx := strings.Index(strContent, target)
	if idx < 0 {
		return fmt.Errorf("unable to find the code %q to be uncomment", target)
	}

	out := new(bytes.Buffer)
	_, err = out.Write(content[:idx])
	if err != nil {
		return fmt.Errorf("failed to write to output: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewBufferString(target))
	if !scanner.Scan() {
		return nil
	}
	for {
		if _, err = out.WriteString(strings.TrimPrefix(scanner.Text(), prefix)); err != nil {
			return fmt.Errorf("failed to write to output: %w", err)
		}
		// Avoid writing a newline in case the previous line was the last in target.
		if !scanner.Scan() {
			break
		}
		if _, err = out.WriteString("\n"); err != nil {
			return fmt.Errorf("failed to write to output: %w", err)
		}
	}

	if _, err = out.Write(content[idx+len(target):]); err != nil {
		return fmt.Errorf("failed to write to output: %w", err)
	}

	// false positive
	// nolint:gosec
	if err = os.WriteFile(filename, out.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write file %q: %w", filename, err)
	}

	return nil
}

// StringReader returns an io.Reader from a string, useful for piping YAML to kubectl
func StringReader(s string) io.Reader {
	return strings.NewReader(s)
}

// InstallKubeVirt installs KubeVirt operator and CR
func InstallKubeVirt() error {
	operatorURL := fmt.Sprintf(kubevirtOperatorURLTmpl, kubevirtVersion)
	crURL := fmt.Sprintf(kubevirtCRURLTmpl, kubevirtVersion)

	_, _ = fmt.Fprintf(os.Stderr, "Installing KubeVirt operator...\n")
	cmd := exec.Command("kubectl", "apply", "-f", operatorURL)
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed to install KubeVirt operator: %w", err)
	}

	// Wait for operator to be ready
	cmd = exec.Command("kubectl", "wait", "deployment/virt-operator",
		"--for", "condition=Available",
		"--namespace", "kubevirt",
		"--timeout", "5m")
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed waiting for virt-operator: %w", err)
	}

	_, _ = fmt.Fprintf(os.Stderr, "Installing KubeVirt CR...\n")
	cmd = exec.Command("kubectl", "apply", "-f", crURL)
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed to install KubeVirt CR: %w", err)
	}

	// Enable software emulation for Kind cluster compatibility
	_, _ = fmt.Fprintf(os.Stderr, "Enabling software emulation in KubeVirt...\n")
	cmd = exec.Command("kubectl", "patch", "kv/kubevirt",
		"-n", "kubevirt",
		"--type=merge",
		"-p", `{"spec":{"configuration":{"developerConfiguration":{"useEmulation":true}}}}`)
	if _, err := Run(cmd); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: Failed to enable software emulation: %v\n", err)
		_, _ = fmt.Fprintf(os.Stderr, "VMs may not run without KVM support\n")
	}

	// Wait for KubeVirt to be ready (with shorter timeout for Kind compatibility)
	// Note: In Kind, virt-handler may fail due to USB device passthrough issues,
	// but the CRDs are still usable for backup/restore testing
	cmd = exec.Command("kubectl", "wait", "kv/kubevirt",
		"--for", "condition=Available",
		"--namespace", "kubevirt",
		"--timeout", "2m")
	if _, err := Run(cmd); err != nil {
		// For testing purposes, we can continue even if KubeVirt isn't fully ready
		// as long as the CRDs are installed
		_, _ = fmt.Fprintf(os.Stderr,
			"Warning: KubeVirt did not become fully Available (this is expected in Kind): %v\n", err)
		_, _ = fmt.Fprintf(os.Stderr, "Continuing anyway as CRDs should be installed...\n")
	}

	return nil
}

// UninstallKubeVirt uninstalls KubeVirt
func UninstallKubeVirt() {
	crURL := fmt.Sprintf(kubevirtCRURLTmpl, kubevirtVersion)
	operatorURL := fmt.Sprintf(kubevirtOperatorURLTmpl, kubevirtVersion)

	cmd := exec.Command("kubectl", "delete", "-f", crURL, "--ignore-not-found")
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}

	cmd = exec.Command("kubectl", "delete", "-f", operatorURL, "--ignore-not-found")
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}
}

// IsKubeVirtInstalled checks if KubeVirt is installed
func IsKubeVirtInstalled() bool {
	cmd := exec.Command("kubectl", "get", "kv", "kubevirt", "-n", "kubevirt")
	_, err := Run(cmd)
	return err == nil
}

// InstallCDI installs Containerized Data Importer operator and CR
func InstallCDI() error {
	operatorURL := fmt.Sprintf(cdiOperatorURLTmpl, cdiVersion)
	crURL := fmt.Sprintf(cdiCRURLTmpl, cdiVersion)

	_, _ = fmt.Fprintf(os.Stderr, "Installing CDI operator...\n")
	cmd := exec.Command("kubectl", "apply", "-f", operatorURL)
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed to install CDI operator: %w", err)
	}

	// Wait for operator to be ready
	cmd = exec.Command("kubectl", "wait", "deployment/cdi-operator",
		"--for", "condition=Available",
		"--namespace", "cdi",
		"--timeout", "5m")
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed waiting for cdi-operator: %w", err)
	}

	_, _ = fmt.Fprintf(os.Stderr, "Installing CDI CR...\n")
	cmd = exec.Command("kubectl", "apply", "-f", crURL)
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed to install CDI CR: %w", err)
	}

	// Wait for CDI to be ready (with shorter timeout for Kind compatibility)
	// Note: In Kind, CDI components may have issues but CRDs are still usable
	cmd = exec.Command("kubectl", "wait", "cdi/cdi",
		"--for", "condition=Available",
		"--namespace", "cdi",
		"--timeout", "2m")
	if _, err := Run(cmd); err != nil {
		// For testing purposes, we can continue even if CDI isn't fully ready
		// as long as the CRDs are installed
		_, _ = fmt.Fprintf(os.Stderr, "Warning: CDI did not become fully Available (this may be expected in Kind): %v\n", err)
		_, _ = fmt.Fprintf(os.Stderr, "Continuing anyway as CRDs should be installed...\n")
	}

	return nil
}

// UninstallCDI uninstalls CDI
func UninstallCDI() {
	crURL := fmt.Sprintf(cdiCRURLTmpl, cdiVersion)
	operatorURL := fmt.Sprintf(cdiOperatorURLTmpl, cdiVersion)

	cmd := exec.Command("kubectl", "delete", "-f", crURL, "--ignore-not-found")
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}

	cmd = exec.Command("kubectl", "delete", "-f", operatorURL, "--ignore-not-found")
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}
}

// IsCDIInstalled checks if CDI is installed
func IsCDIInstalled() bool {
	cmd := exec.Command("kubectl", "get", "cdi", "cdi", "-n", "cdi")
	_, err := Run(cmd)
	return err == nil
}

// InstallMinIO installs MinIO for backup storage in tests
func InstallMinIO() error {
	_, _ = fmt.Fprintf(os.Stderr, "Installing MinIO for backup storage...\n")

	minioYAML := `
apiVersion: v1
kind: Namespace
metadata:
  name: minio
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: minio-pvc
  namespace: minio
spec:
  accessModes:
  - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: minio
  namespace: minio
spec:
  selector:
    matchLabels:
      app: minio
  template:
    metadata:
      labels:
        app: minio
    spec:
      containers:
      - name: minio
        image: ` + minioImage + `
        args:
        - server
        - /storage
        env:
        - name: MINIO_ACCESS_KEY
          value: "minio"
        - name: MINIO_SECRET_KEY
          value: "minio123"
        ports:
        - containerPort: 9000
        volumeMounts:
        - name: storage
          mountPath: /storage
      volumes:
      - name: storage
        persistentVolumeClaim:
          claimName: minio-pvc
---
apiVersion: v1
kind: Service
metadata:
  name: minio
  namespace: minio
spec:
  type: ClusterIP
  ports:
  - port: 9000
    targetPort: 9000
  selector:
    app: minio
`
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = StringReader(minioYAML)
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed to install MinIO: %w", err)
	}

	// Wait for MinIO to be ready
	cmd = exec.Command("kubectl", "wait", "deployment/minio",
		"--for", "condition=Available",
		"--namespace", "minio",
		"--timeout", "5m")
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed waiting for MinIO: %w", err)
	}

	// Create bucket using a job
	createBucketYAML := `
apiVersion: batch/v1
kind: Job
metadata:
  name: create-bucket
  namespace: minio
spec:
  template:
    spec:
      restartPolicy: OnFailure
      containers:
      - name: mc
        image: minio/mc:latest
        command:
        - /bin/sh
        - -c
        - |
          mc alias set myminio http://minio.minio.svc:9000 minio minio123
          mc mb myminio/velero || true
          mc version enable myminio/velero || true
`
	cmd = exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = StringReader(createBucketYAML)
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed to create bucket job: %w", err)
	}

	// Wait for job to complete
	cmd = exec.Command("kubectl", "wait", "job/create-bucket",
		"--for", "condition=Complete",
		"--namespace", "minio",
		"--timeout", "2m")
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed waiting for bucket creation: %w", err)
	}

	_, _ = fmt.Fprintf(os.Stderr, "MinIO installed successfully\n")
	return nil
}

// UninstallMinIO uninstalls MinIO
func UninstallMinIO() {
	cmd := exec.Command("kubectl", "delete", "namespace", "minio", "--ignore-not-found")
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}
}

// InstallVelero installs Velero with kubevirt and openshift plugins
func InstallVelero() error {
	_, _ = fmt.Fprintf(os.Stderr, "Installing Velero with plugins...\n")

	// Create openshift-adp namespace
	cmd := exec.Command("kubectl", "create", "namespace", "openshift-adp")
	if _, err := Run(cmd); err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("failed to create openshift-adp namespace: %w", err)
		}
	}

	// Create credentials secret for MinIO
	credentialsYAML := `
apiVersion: v1
kind: Secret
metadata:
  name: cloud-credentials
  namespace: openshift-adp
type: Opaque
stringData:
  cloud: |
    [default]
    aws_access_key_id=minio
    aws_secret_access_key=minio123
`
	cmd = exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = StringReader(credentialsYAML)
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed to create credentials: %w", err)
	}

	// Install Velero CRDs
	cmd = exec.Command("kubectl", "apply", "-f", "hack/extra-crds/")
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed to install Velero CRDs: %w", err)
	}

	// Install additional Velero CRDs from upstream that are not in hack/extra-crds/
	// These are required for Velero to start successfully
	baseURL := "https://raw.githubusercontent.com/vmware-tanzu/velero/" + veleroVersion
	additionalCRDs := []string{
		baseURL + "/config/crd/v1/bases/velero.io_serverstatusrequests.yaml",
		baseURL + "/config/crd/v1/bases/velero.io_schedules.yaml",
		baseURL + "/config/crd/v1/bases/velero.io_podvolumerestores.yaml",
		baseURL + "/config/crd/v1/bases/velero.io_deletebackuprequests.yaml",
		baseURL + "/config/crd/v1/bases/velero.io_podvolumebackups.yaml",
		baseURL + "/config/crd/v1/bases/velero.io_backuprepositories.yaml",
		baseURL + "/config/crd/v1/bases/velero.io_downloadrequests.yaml",
		baseURL + "/config/crd/v1/bases/velero.io_volumesnapshotlocations.yaml",
		baseURL + "/config/crd/v2alpha1/bases/velero.io_datauploads.yaml",
		baseURL + "/config/crd/v2alpha1/bases/velero.io_datadownloads.yaml",
	}

	for _, crdURL := range additionalCRDs {
		cmd = exec.Command("kubectl", "apply", "-f", crdURL)
		if _, err := Run(cmd); err != nil {
			return fmt.Errorf("failed to install CRD from %s: %w", crdURL, err)
		}
	}

	// Install Velero with plugins
	veleroYAML := `
apiVersion: v1
kind: ServiceAccount
metadata:
  name: velero
  namespace: openshift-adp
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: velero
subjects:
- kind: ServiceAccount
  name: velero
  namespace: openshift-adp
roleRef:
  kind: ClusterRole
  name: cluster-admin
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: velero
  namespace: openshift-adp
spec:
  selector:
    matchLabels:
      app: velero
  template:
    metadata:
      labels:
        app: velero
    spec:
      serviceAccountName: velero
      containers:
      - name: velero
        image: ` + veleroImage + `
        command:
        - /velero
        args:
        - server
        - --default-backup-storage-location=default
        - --default-volume-snapshot-locations=aws:default
        volumeMounts:
        - name: plugins
          mountPath: /plugins
        - name: scratch
          mountPath: /scratch
        - name: cloud-credentials
          mountPath: /credentials
        env:
        - name: VELERO_SCRATCH_DIR
          value: /scratch
        - name: AWS_SHARED_CREDENTIALS_FILE
          value: /credentials/cloud
        - name: VELERO_NAMESPACE
          value: openshift-adp
      initContainers:
      - name: velero-plugin-for-aws
        image: ` + awsPluginImage + `
        volumeMounts:
        - name: plugins
          mountPath: /target
      - name: kubevirt-velero-plugin
        image: ` + kubevirtPluginImage + `
        volumeMounts:
        - name: plugins
          mountPath: /target
      volumes:
      - name: plugins
        emptyDir: {}
      - name: scratch
        emptyDir: {}
      - name: cloud-credentials
        secret:
          secretName: cloud-credentials
`
	cmd = exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = StringReader(veleroYAML)
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed to deploy Velero: %w", err)
	}

	// Wait for Velero to be ready
	cmd = exec.Command("kubectl", "wait", "deployment/velero",
		"--for", "condition=Available",
		"--namespace", "openshift-adp",
		"--timeout", "5m")
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed waiting for Velero: %w", err)
	}

	// Create BackupStorageLocation
	bslYAML := `
apiVersion: velero.io/v1
kind: BackupStorageLocation
metadata:
  name: default
  namespace: openshift-adp
spec:
  provider: aws
  objectStorage:
    bucket: velero
  config:
    region: minio
    s3ForcePathStyle: "true"
    s3Url: http://minio.minio.svc:9000
  credential:
    name: cloud-credentials
    key: cloud
`
	cmd = exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = StringReader(bslYAML)
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed to create BackupStorageLocation: %w", err)
	}

	// Create VolumeSnapshotLocation (for CSI snapshots - won't work in Kind but needed for CR)
	vslYAML := `
apiVersion: velero.io/v1
kind: VolumeSnapshotLocation
metadata:
  name: default
  namespace: openshift-adp
spec:
  provider: aws
  config:
    region: minio
`
	cmd = exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = StringReader(vslYAML)
	if _, err := Run(cmd); err != nil {
		return fmt.Errorf("failed to create VolumeSnapshotLocation: %w", err)
	}

	// Wait for BackupStorageLocation to be Available
	// This ensures Velero can successfully connect to MinIO before tests run
	_, _ = fmt.Fprintf(os.Stderr, "Waiting for BackupStorageLocation to be ready...\n")
	maxRetries := 60 // 60 * 5s = 5 minutes
	bslReady := false
	var lastOutput string
	for i := 0; i < maxRetries; i++ {
		cmd = exec.Command("kubectl", "get", "backupstoragelocation", "default",
			"-n", "openshift-adp",
			"-o", "jsonpath={.status.phase}")
		output, err := Run(cmd)
		lastOutput = strings.TrimSpace(output)
		if err == nil && lastOutput == "Available" {
			_, _ = fmt.Fprintf(os.Stderr, "BackupStorageLocation is Available\n")
			bslReady = true
			break
		}
		if i < maxRetries-1 {
			time.Sleep(5 * time.Second)
		}
	}

	if !bslReady {
		// Print diagnostic information before failing
		_, _ = fmt.Fprintf(os.Stderr, "\n========== BSL DIAGNOSTIC INFO ==========\n")
		_, _ = fmt.Fprintf(os.Stderr, "BackupStorageLocation not Available after 5 minutes (phase: %s)\n", lastOutput)

		// Get BSL full status
		cmd = exec.Command("kubectl", "get", "backupstoragelocation", "default",
			"-n", "openshift-adp", "-o", "yaml")
		if bslYaml, err := Run(cmd); err == nil {
			_, _ = fmt.Fprintf(os.Stderr, "\nBSL Status:\n%s\n", bslYaml)
		}

		// Get Velero pod logs
		cmd = exec.Command("kubectl", "logs", "-n", "openshift-adp",
			"-l", "app=velero", "--tail=50")
		if logs, err := Run(cmd); err == nil {
			_, _ = fmt.Fprintf(os.Stderr, "\nVelero Controller Logs (last 50 lines):\n%s\n", logs)
		}

		// Check MinIO connectivity
		cmd = exec.Command("kubectl", "get", "pods", "-n", "minio")
		if minioPods, err := Run(cmd); err == nil {
			_, _ = fmt.Fprintf(os.Stderr, "\nMinIO Pods:\n%s\n", minioPods)
		}

		_, _ = fmt.Fprintf(os.Stderr, "========================================\n\n")

		return fmt.Errorf("BackupStorageLocation did not become Available (phase: %s). "+
			"Velero cannot process backups without a valid storage location. "+
			"Check the diagnostic output above", lastOutput)
	}

	_, _ = fmt.Fprintf(os.Stderr, "Velero with plugins installed successfully\n")
	return nil
}

// UninstallVelero uninstalls Velero
func UninstallVelero() {
	cmd := exec.Command("kubectl", "delete", "namespace", "openshift-adp", "--ignore-not-found")
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}
}

// IsVeleroInstalled checks if Velero namespace exists
func IsVeleroInstalled() bool {
	cmd := exec.Command("kubectl", "get", "namespace", "openshift-adp")
	_, err := Run(cmd)
	return err == nil
}
