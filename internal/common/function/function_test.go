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

package function

import (
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"golang.org/x/crypto/ssh"
	apitypes "k8s.io/apimachinery/pkg/types"

	"github.com/migtools/oadp-vm-file-restore/internal/common/constant"
)

const (
	testUsername = "testuser"
	testVMFRName = "test-vmfr"
)

func TestGenerateSSHKeyPair(t *testing.T) {
	logger := logr.Discard()

	t.Run("generates valid keypair", func(t *testing.T) {
		keyPair, err := GenerateSSHKeyPair(logger)
		if err != nil {
			t.Fatalf("GenerateSSHKeyPair() error = %v", err)
		}

		// Verify private key format
		if !strings.Contains(keyPair.PrivateKey, "-----BEGIN PRIVATE KEY-----") {
			t.Error("Private key missing PEM header")
		}
		if !strings.Contains(keyPair.PrivateKey, "-----END PRIVATE KEY-----") {
			t.Error("Private key missing PEM footer")
		}

		// Verify public key format (should start with ssh-ed25519)
		if !strings.HasPrefix(keyPair.PublicKey, "ssh-ed25519") {
			t.Errorf("Public key has wrong format, got prefix: %s", keyPair.PublicKey[:20])
		}

		// Verify public key can be parsed by SSH package
		_, _, _, _, err = ssh.ParseAuthorizedKey([]byte(keyPair.PublicKey))
		if err != nil {
			t.Errorf("Public key is not valid SSH format: %v", err)
		}
	})

	t.Run("generates unique keypairs", func(t *testing.T) {
		keyPair1, err := GenerateSSHKeyPair(logger)
		if err != nil {
			t.Fatalf("GenerateSSHKeyPair() error = %v", err)
		}

		keyPair2, err := GenerateSSHKeyPair(logger)
		if err != nil {
			t.Fatalf("GenerateSSHKeyPair() error = %v", err)
		}

		// Each call should generate different keys
		if keyPair1.PublicKey == keyPair2.PublicKey {
			t.Error("Generated identical public keys")
		}
		if keyPair1.PrivateKey == keyPair2.PrivateKey {
			t.Error("Generated identical private keys")
		}
	})
}

func TestGenerateFileBrowserCredentials(t *testing.T) {
	logger := logr.Discard()

	t.Run("generates credentials with default username", func(t *testing.T) {
		creds, err := GenerateFileBrowserCredentials("", logger)
		if err != nil {
			t.Fatalf("GenerateFileBrowserCredentials() error = %v", err)
		}

		if creds.Username != "oadp" {
			t.Errorf("Expected default username 'oadp', got '%s'", creds.Username)
		}

		// Password should be non-empty
		if creds.Password == "" {
			t.Error("Generated empty password")
		}

		// Password should be base64-encoded (at least 40 chars for 32 bytes)
		if len(creds.Password) < 40 {
			t.Errorf("Password too short, got length: %d", len(creds.Password))
		}
	})

	t.Run("generates credentials with custom username", func(t *testing.T) {
		creds, err := GenerateFileBrowserCredentials(testUsername, logger)
		if err != nil {
			t.Fatalf("GenerateFileBrowserCredentials() error = %v", err)
		}

		if creds.Username != testUsername {
			t.Errorf("Expected username 'testuser', got '%s'", creds.Username)
		}
	})

	t.Run("generates unique passwords", func(t *testing.T) {
		creds1, err := GenerateFileBrowserCredentials("user1", logger)
		if err != nil {
			t.Fatalf("GenerateFileBrowserCredentials() error = %v", err)
		}

		creds2, err := GenerateFileBrowserCredentials("user2", logger)
		if err != nil {
			t.Fatalf("GenerateFileBrowserCredentials() error = %v", err)
		}

		// Each call should generate different passwords
		if creds1.Password == creds2.Password {
			t.Error("Generated identical passwords")
		}
	})
}

func TestCreateSSHCredentialsSecret(t *testing.T) {
	logger := logr.Discard()

	keyPair := &SSHKeyPair{
		PrivateKey: "test-private-key",
		PublicKey:  "test-public-key",
	}

	secret := CreateSSHCredentialsSecret(
		"test-secret",
		"test-namespace",
		testUsername,
		keyPair,
		testVMFRName,
		"vmfr-namespace",
		apitypes.UID("test-uid"),
		logger,
	)

	t.Run("creates secret with correct metadata", func(t *testing.T) {
		if secret.Name != "test-secret" {
			t.Errorf("Expected name 'test-secret', got '%s'", secret.Name)
		}
		if secret.Namespace != "test-namespace" {
			t.Errorf("Expected namespace 'test-namespace', got '%s'", secret.Namespace)
		}

		// Check labels
		if secret.Labels["oadp.openshift.io/credential-type"] != "ssh" {
			t.Error("Missing or incorrect credential-type label")
		}
		if secret.Labels[constant.VMFROriginUUIDLabel] != "test-uid" {
			t.Errorf("Expected UUID label '%s', got '%s'", "test-uid", secret.Labels[constant.VMFROriginUUIDLabel])
		}

		// Check annotations
		if secret.Annotations[constant.VMFROriginNameAnnotation] != testVMFRName {
			t.Errorf("Expected name annotation 'test-vmfr', got '%s'", secret.Annotations[constant.VMFROriginNameAnnotation])
		}
		if secret.Annotations[constant.VMFROriginNamespaceAnnotation] != "vmfr-namespace" {
			t.Errorf("Expected namespace annotation 'vmfr-namespace', got '%s'", secret.Annotations[constant.VMFROriginNamespaceAnnotation])
		}
	})

	t.Run("creates secret with correct data", func(t *testing.T) {
		if secret.StringData["username"] != testUsername {
			t.Errorf("Expected username 'testuser', got '%s'", secret.StringData["username"])
		}
		if secret.StringData["privateKey"] != "test-private-key" {
			t.Error("Private key not stored correctly")
		}
		if secret.StringData["publicKey"] != "test-public-key" {
			t.Error("Public key not stored correctly")
		}
	})

	t.Run("creates secret with owner reference", func(t *testing.T) {
		if len(secret.OwnerReferences) != 1 {
			t.Fatalf("Expected 1 owner reference, got %d", len(secret.OwnerReferences))
		}

		owner := secret.OwnerReferences[0]
		if owner.Name != testVMFRName {
			t.Errorf("Expected owner name 'test-vmfr', got '%s'", owner.Name)
		}
		if owner.Kind != "VirtualMachineFileRestore" {
			t.Errorf("Expected owner kind 'VirtualMachineFileRestore', got '%s'", owner.Kind)
		}
		if *owner.Controller != true {
			t.Error("Owner reference should be controller")
		}
	})
}

func TestCreateFileBrowserCredentialsSecret(t *testing.T) {
	logger := logr.Discard()

	creds := &FileBrowserCredentials{
		Username: testUsername,
		Password: "testpassword",
	}

	secret := CreateFileBrowserCredentialsSecret(
		"fb-secret",
		"test-namespace",
		creds,
		testVMFRName,
		"vmfr-namespace",
		apitypes.UID("test-uid"),
		logger,
	)

	t.Run("creates secret with correct metadata", func(t *testing.T) {
		if secret.Name != "fb-secret" {
			t.Errorf("Expected name 'fb-secret', got '%s'", secret.Name)
		}

		// Check labels
		if secret.Labels["oadp.openshift.io/credential-type"] != "filebrowser" {
			t.Error("Missing or incorrect credential-type label")
		}
		if secret.Labels[constant.VMFROriginUUIDLabel] != "test-uid" {
			t.Errorf("Expected UUID label '%s', got '%s'", "test-uid", secret.Labels[constant.VMFROriginUUIDLabel])
		}

		// Check annotations
		if secret.Annotations[constant.VMFROriginNameAnnotation] != testVMFRName {
			t.Errorf("Expected name annotation 'test-vmfr', got '%s'", secret.Annotations[constant.VMFROriginNameAnnotation])
		}
		if secret.Annotations[constant.VMFROriginNamespaceAnnotation] != "vmfr-namespace" {
			t.Errorf("Expected namespace annotation 'vmfr-namespace', got '%s'", secret.Annotations[constant.VMFROriginNamespaceAnnotation])
		}
	})

	t.Run("creates secret with correct data", func(t *testing.T) {
		if secret.StringData["username"] != testUsername {
			t.Errorf("Expected username 'testuser', got '%s'", secret.StringData["username"])
		}
		if secret.StringData["password"] != "testpassword" {
			t.Error("Password not stored correctly")
		}
	})
}

func TestGenerateTemporaryVMFRNamespaceName(t *testing.T) {
	logger := logr.Discard()

	t.Run("generates name with all components", func(t *testing.T) {
		name := GenerateTemporaryVMFRNamespaceName(
			"restore",
			"vm-namespace",
			"test-vm",
			"12345678-1234-5678-9012-123456789012",
			logger,
		)

		expected := "restore-vm-namespace-test-vm-12345678"
		if name != expected {
			t.Errorf("Expected '%s', got '%s'", expected, name)
		}
	})

	t.Run("generates name without prefix", func(t *testing.T) {
		name := GenerateTemporaryVMFRNamespaceName(
			"",
			"vm-namespace",
			"test-vm",
			"12345678-1234-5678-9012-123456789012",
			logger,
		)

		expected := "vm-namespace-test-vm-12345678"
		if name != expected {
			t.Errorf("Expected '%s', got '%s'", expected, name)
		}
	})

	t.Run("generates DNS-1123 compliant names", func(t *testing.T) {
		name := GenerateTemporaryVMFRNamespaceName(
			"Test_Prefix",
			"Test_Namespace",
			"Test_VM",
			"ABCD1234",
			logger,
		)

		// Should convert to lowercase and replace invalid chars
		expected := "test-prefix-test-namespace-test-vm-abcd1234"
		if name != expected {
			t.Errorf("Expected '%s', got '%s'", expected, name)
		}

		// Should be valid DNS-1123 label
		if len(name) > 63 {
			t.Errorf("Name too long (>63 chars): %d", len(name))
		}
		if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
			t.Error("Name should not start or end with hyphen")
		}
	})

	t.Run("truncates long names while preserving suffix", func(t *testing.T) {
		longPrefix := "very-long-prefix-that-exceeds-normal-length-limits"
		longNamespace := "very-long-namespace-name-that-also-exceeds-limits"
		longVMName := "very-long-vm-name-that-will-make-total-length-exceed-63-chars"

		name := GenerateTemporaryVMFRNamespaceName(
			longPrefix,
			longNamespace,
			longVMName,
			"12345678",
			logger,
		)

		// Should truncate but keep the suffix
		if len(name) > 63 {
			t.Errorf("Name not truncated properly, length: %d", len(name))
		}
		if !strings.HasSuffix(name, "12345678") {
			t.Error("Truncated name should preserve UID suffix")
		}
	})
}

func TestGenerateVeleroRestorePrefix(t *testing.T) {
	logger := logr.Discard()

	t.Run("generates correct prefix", func(t *testing.T) {
		prefix := GenerateVeleroRestorePrefix(
			testVMFRName,
			"test-backup",
			logger,
		)

		expected := "vmfr-test-vmfr-test-backup-"
		if prefix != expected {
			t.Errorf("Expected '%s', got '%s'", expected, prefix)
		}
	})

	t.Run("converts to lowercase", func(t *testing.T) {
		prefix := GenerateVeleroRestorePrefix(
			"Test-VMFR",
			"Test-Backup",
			logger,
		)

		expected := "vmfr-test-vmfr-test-backup-"
		if prefix != expected {
			t.Errorf("Expected lowercase '%s', got '%s'", expected, prefix)
		}
	})
}
