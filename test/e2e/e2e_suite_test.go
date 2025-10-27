//go:build e2e
// +build e2e

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

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/migtools/oadp-vm-file-restore/test/utils"
)

var (
	// Optional Environment Variables:
	// - CERT_MANAGER_INSTALL_SKIP=true: Skips CertManager installation during test setup.
	// - KUBEVIRT_INSTALL_SKIP=true: Skips KubeVirt installation during test setup.
	// - CDI_INSTALL_SKIP=true: Skips CDI installation during test setup.
	// - MINIO_INSTALL_SKIP=true: Skips MinIO installation during test setup.
	// - VELERO_INSTALL_SKIP=true: Skips Velero installation during test setup.
	// These variables are useful if components are already installed, avoiding
	// re-installation and conflicts.
	skipCertManagerInstall = os.Getenv("CERT_MANAGER_INSTALL_SKIP") == "true"
	skipKubeVirtInstall    = os.Getenv("KUBEVIRT_INSTALL_SKIP") == "true"
	skipCDIInstall         = os.Getenv("CDI_INSTALL_SKIP") == "true"
	skipMinIOInstall       = os.Getenv("MINIO_INSTALL_SKIP") == "true"
	skipVeleroInstall      = os.Getenv("VELERO_INSTALL_SKIP") == "true"

	// Track what was installed by the test suite (vs already present)
	isCertManagerAlreadyInstalled = false
	isKubeVirtAlreadyInstalled    = false
	isCDIAlreadyInstalled          = false
	isMinIOAlreadyInstalled        = false
	isVeleroAlreadyInstalled       = false

	// projectImage is the name of the image which will be build and loaded
	// with the code source changes to be tested.
	projectImage = "localhost/oadp-vm-file-restore:dev"
)

// TestE2E runs the end-to-end (e2e) test suite for the project. These tests execute in an isolated,
// temporary environment to validate project changes with the purpose of being used in CI jobs.
// The default setup requires Kind, builds/loads the Manager Docker image locally, and installs
// CertManager.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting oadp-vm-file-restore integration test suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	// Check system limits before running tests
	By("checking system inotify limits")
	checkInotifyLimits()

	By("building the manager(Operator) image")
	cmd := exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", projectImage))
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the manager(Operator) image")

	By("loading the manager(Operator) image on Kind")
	err = utils.LoadImageToKindClusterWithName(projectImage)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the manager(Operator) image into Kind")

	// The tests-e2e are intended to run on a temporary cluster that is created and destroyed for testing.
	// To prevent errors when tests run in environments with components already installed,
	// we check for their presence before execution.

	// Setup CertManager
	if !skipCertManagerInstall {
		By("checking if cert manager is installed already")
		isCertManagerAlreadyInstalled = utils.IsCertManagerCRDsInstalled()
		if !isCertManagerAlreadyInstalled {
			_, _ = fmt.Fprintf(GinkgoWriter, "Installing CertManager...\n")
			Expect(utils.InstallCertManager()).To(Succeed(), "Failed to install CertManager")
		} else {
			_, _ = fmt.Fprintf(GinkgoWriter, "WARNING: CertManager is already installed. Skipping installation...\n")
		}
	}

	// Setup KubeVirt
	if !skipKubeVirtInstall {
		By("checking if KubeVirt is installed already")
		isKubeVirtAlreadyInstalled = utils.IsKubeVirtInstalled()
		if !isKubeVirtAlreadyInstalled {
			_, _ = fmt.Fprintf(GinkgoWriter, "Installing KubeVirt...\n")
			Expect(utils.InstallKubeVirt()).To(Succeed(), "Failed to install KubeVirt")
		} else {
			_, _ = fmt.Fprintf(GinkgoWriter, "WARNING: KubeVirt is already installed. Skipping installation...\n")
		}
	}

	// Setup CDI (Containerized Data Importer)
	if !skipCDIInstall {
		By("checking if CDI is installed already")
		isCDIAlreadyInstalled = utils.IsCDIInstalled()
		if !isCDIAlreadyInstalled {
			_, _ = fmt.Fprintf(GinkgoWriter, "Installing CDI...\n")
			Expect(utils.InstallCDI()).To(Succeed(), "Failed to install CDI")
		} else {
			_, _ = fmt.Fprintf(GinkgoWriter, "WARNING: CDI is already installed. Skipping installation...\n")
		}
	}

	// Setup MinIO (backup storage for Velero)
	if !skipMinIOInstall {
		By("checking if MinIO is installed already")
		cmd := exec.Command("kubectl", "get", "namespace", "minio")
		_, err = utils.Run(cmd)
		isMinIOAlreadyInstalled = (err == nil)
		if !isMinIOAlreadyInstalled {
			_, _ = fmt.Fprintf(GinkgoWriter, "Installing MinIO...\n")
			Expect(utils.InstallMinIO()).To(Succeed(), "Failed to install MinIO")
		} else {
			_, _ = fmt.Fprintf(GinkgoWriter, "WARNING: MinIO is already installed. Skipping installation...\n")
		}
	}

	// Setup Velero (with kubevirt and openshift plugins)
	if !skipVeleroInstall {
		By("checking if Velero is installed already")
		isVeleroAlreadyInstalled = utils.IsVeleroInstalled()
		if !isVeleroAlreadyInstalled {
			_, _ = fmt.Fprintf(GinkgoWriter, "Installing Velero with plugins...\n")
			Expect(utils.InstallVelero()).To(Succeed(), "Failed to install Velero")
		} else {
			_, _ = fmt.Fprintf(GinkgoWriter, "WARNING: Velero is already installed. Skipping installation...\n")
		}
	}
})

var _ = AfterSuite(func() {
	// Teardown components in reverse order if they were installed by this test suite

	if !skipVeleroInstall && !isVeleroAlreadyInstalled {
		_, _ = fmt.Fprintf(GinkgoWriter, "Uninstalling Velero...\n")
		utils.UninstallVelero()
	}

	if !skipMinIOInstall && !isMinIOAlreadyInstalled {
		_, _ = fmt.Fprintf(GinkgoWriter, "Uninstalling MinIO...\n")
		utils.UninstallMinIO()
	}

	if !skipCDIInstall && !isCDIAlreadyInstalled {
		_, _ = fmt.Fprintf(GinkgoWriter, "Uninstalling CDI...\n")
		utils.UninstallCDI()
	}

	if !skipKubeVirtInstall && !isKubeVirtAlreadyInstalled {
		_, _ = fmt.Fprintf(GinkgoWriter, "Uninstalling KubeVirt...\n")
		utils.UninstallKubeVirt()
	}

	if !skipCertManagerInstall && !isCertManagerAlreadyInstalled {
		_, _ = fmt.Fprintf(GinkgoWriter, "Uninstalling CertManager...\n")
		utils.UninstallCertManager()
	}
})

// checkInotifyLimits warns if system inotify limits are too low for running tests
func checkInotifyLimits() {
	const (
		minInstances = 1024
		minWatches   = 524288
	)

	// Read max_user_instances
	cmd := exec.Command("sysctl", "-n", "fs.inotify.max_user_instances")
	output, err := cmd.CombinedOutput()
	if err == nil {
		instances, err := strconv.Atoi(strings.TrimSpace(string(output)))
		if err == nil && instances < minInstances {
			_, _ = fmt.Fprintf(GinkgoWriter, "\n")
			_, _ = fmt.Fprintf(GinkgoWriter, "========================================================================\n")
			_, _ = fmt.Fprintf(GinkgoWriter, "WARNING: System inotify limits are too low!\n")
			_, _ = fmt.Fprintf(GinkgoWriter, "Current: fs.inotify.max_user_instances = %d\n", instances)
			_, _ = fmt.Fprintf(GinkgoWriter, "Recommended: %d or higher\n", minInstances)
			_, _ = fmt.Fprintf(GinkgoWriter, "\n")
			_, _ = fmt.Fprintf(GinkgoWriter, "This will likely cause 'too many files open' errors and test timeouts.\n")
			_, _ = fmt.Fprintf(GinkgoWriter, "\n")
			_, _ = fmt.Fprintf(GinkgoWriter, "To fix, run:\n")
			_, _ = fmt.Fprintf(GinkgoWriter, "  sudo sysctl fs.inotify.max_user_instances=%d\n", minInstances)
			_, _ = fmt.Fprintf(GinkgoWriter, "  sudo sysctl fs.inotify.max_user_watches=%d\n", minWatches)
			_, _ = fmt.Fprintf(GinkgoWriter, "\n")
			_, _ = fmt.Fprintf(GinkgoWriter, "See test/e2e/KIND_LIMITATIONS.md for more details.\n")
			_, _ = fmt.Fprintf(GinkgoWriter, "========================================================================\n")
			_, _ = fmt.Fprintf(GinkgoWriter, "\n")
		}
	}

	// Read max_user_watches
	cmd = exec.Command("sysctl", "-n", "fs.inotify.max_user_watches")
	output, err = cmd.CombinedOutput()
	if err == nil {
		watches, err := strconv.Atoi(strings.TrimSpace(string(output)))
		if err == nil && watches < minWatches {
			_, _ = fmt.Fprintf(GinkgoWriter, "WARNING: fs.inotify.max_user_watches = %d (recommended: %d)\n", watches, minWatches)
		}
	}
}
