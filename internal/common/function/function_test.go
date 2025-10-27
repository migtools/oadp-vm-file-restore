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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		"test-secret-",
		"test-namespace",
		testUsername,
		keyPair,
		testVMFRName,
		"vmfr-namespace",
		apitypes.UID("test-uid"),
		logger,
	)

	t.Run("creates secret with correct metadata", func(t *testing.T) {
		if secret.GenerateName != "test-secret-" {
			t.Errorf("Expected generateName 'test-secret-', got '%s'", secret.GenerateName)
		}
		if secret.Namespace != "test-namespace" {
			t.Errorf("Expected namespace 'test-namespace', got '%s'", secret.Namespace)
		}

		// Check labels
		if secret.Labels[constant.CredentialTypeLabel] != constant.CredentialTypeSSH {
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
		if secret.StringData["authorized_keys"] != "test-public-key" {
			t.Error("Public key not stored correctly")
		}
	})

	t.Run("creates secret without owner reference", func(t *testing.T) {
		// Owner references are not set for cross-namespace resources
		// (VMFR may be in different namespace than secret)
		if len(secret.OwnerReferences) != 0 {
			t.Fatalf("Expected 0 owner references (cross-namespace not allowed), got %d", len(secret.OwnerReferences))
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
		"fb-secret-",
		"test-namespace",
		creds,
		testVMFRName,
		"vmfr-namespace",
		apitypes.UID("test-uid"),
		logger,
	)

	t.Run("creates secret with correct metadata", func(t *testing.T) {
		if secret.GenerateName != "fb-secret-" {
			t.Errorf("Expected generateName 'fb-secret-', got '%s'", secret.GenerateName)
		}

		// Check labels
		if secret.Labels[constant.CredentialTypeLabel] != constant.CredentialTypeFileBrowser {
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

func TestValidateSSHPublicKey(t *testing.T) {
	t.Run("validates valid ED25519 public key", func(t *testing.T) {
		// Valid ED25519 public key
		validKey := []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test@example.com")
		err := ValidateSSHPublicKey(validKey)
		if err != nil {
			t.Errorf("Expected valid key to pass validation, got error: %v", err)
		}
	})

	t.Run("validates valid ECDSA P-256 public key", func(t *testing.T) {
		// Valid ECDSA P-256 public key
		validKey := []byte("ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBEmKSENjQEezOmxkZMy7opKgwFB9nkt5YRrYMjNuG5N87uRgg6CLrbo5wAdT/y6v0mKV0U2w0WZ2YB/++Tpockg= test@example.com")
		err := ValidateSSHPublicKey(validKey)
		if err != nil {
			t.Errorf("Expected valid ECDSA key to pass validation, got error: %v", err)
		}
	})

	t.Run("validates valid RSA public key", func(t *testing.T) {
		// Valid RSA key - modern SSH will use SHA-2 signatures (rsa-sha2-256/512) at runtime
		// All RSA keys in authorized_keys format are labeled "ssh-rsa" regardless of signature algorithm
		rsaKey := []byte("ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCgdRkm8/liQKXLAUVsC9ohk+TJk0/lJIy/7jDK0VoZtK9mnEDCxtsk1swu9n4q5yzg6kDevOSvdy+RmBddMc9P4QMbvmXLkXwq7JXIDAGuS80xwXDl1TtvwT760uuhmSD9jBNYiD26+p+YAvutFcr3XDgv4JFuLs7oSgSnRxOd2JOdx8n/XMzWBsADVNErfZ1+AqF37JI6wxn2eVCgbSJRQguA2uk2V1DWvhaptGXGYyUjjuwcVh525gGwqEYaaokzkTha8WsjJRE1Edlj2j34C3j+5p/vIASbYy0ah20j1Qi1aYplOkOIZcT3t+NFR9cqvwN//bhBQJrYXGS9c781 test@example.com")
		err := ValidateSSHPublicKey(rsaKey)
		if err != nil {
			t.Errorf("Expected valid RSA key to pass validation, got error: %v", err)
		}
	})

	t.Run("rejects dss public key (deprecated)", func(t *testing.T) {
		// Valid DSS key but should be rejected as it's not in allowed list
		dssKey := []byte("ssh-dss AAAAB3NzaC1kc3MAAACBAKYQsDfLsXbNmHfK test@example.com")
		err := ValidateSSHPublicKey(dssKey)
		if err == nil {
			t.Error("Expected ssh-dss key to be rejected")
		}
	})

	t.Run("rejects invalid public key format", func(t *testing.T) {
		invalidKey := []byte("not-a-valid-ssh-key")
		err := ValidateSSHPublicKey(invalidKey)
		if err == nil {
			t.Error("Expected invalid key to fail validation")
		}
	})

	t.Run("rejects empty public key", func(t *testing.T) {
		err := ValidateSSHPublicKey([]byte(""))
		if err == nil {
			t.Error("Expected empty key to fail validation")
		}
	})

	t.Run("rejects malformed key with ssh- prefix", func(t *testing.T) {
		// Has ssh- prefix but is not a valid key
		invalidKey := []byte("ssh-ed25519 invalid-base64-data")
		err := ValidateSSHPublicKey(invalidKey)
		if err == nil {
			t.Error("Expected malformed key to fail validation")
		}
	})
}

func TestValidateSSHSecret(t *testing.T) {
	logger := logr.Discard()

	t.Run("validates secret with valid public key", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "test-ns",
			},
			Data: map[string][]byte{
				"authorized_keys": []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test@example.com"),
			},
		}

		err := ValidateSSHSecret(secret, logger)
		if err != nil {
			t.Errorf("Expected valid secret to pass validation, got error: %v", err)
		}
	})

	t.Run("validates secret with public key and username", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "test-ns",
			},
			Data: map[string][]byte{
				"authorized_keys": []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test@example.com"),
				"username":        []byte("testuser"),
			},
		}

		err := ValidateSSHSecret(secret, logger)
		if err != nil {
			t.Errorf("Expected valid secret to pass validation, got error: %v", err)
		}
	})

	t.Run("validates secret with all fields", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "test-ns",
			},
			Data: map[string][]byte{
				"authorized_keys": []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test@example.com"),
				"privateKey":      []byte("-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----"),
				"username":        []byte("testuser"),
			},
		}

		err := ValidateSSHSecret(secret, logger)
		if err != nil {
			t.Errorf("Expected valid secret to pass validation, got error: %v", err)
		}
	})

	t.Run("rejects secret with nil data", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "test-ns",
			},
			Data: nil,
		}

		err := ValidateSSHSecret(secret, logger)
		if err == nil {
			t.Error("Expected secret with nil data to fail validation")
		}
	})

	t.Run("rejects secret without public key", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "test-ns",
			},
			Data: map[string][]byte{
				"username": []byte("testuser"),
			},
		}

		err := ValidateSSHSecret(secret, logger)
		if err == nil {
			t.Error("Expected secret without authorized_keys to fail validation")
		}
	})

	t.Run("rejects secret with empty public key", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "test-ns",
			},
			Data: map[string][]byte{
				"authorized_keys": []byte(""),
			},
		}

		err := ValidateSSHSecret(secret, logger)
		if err == nil {
			t.Error("Expected secret with empty authorized_keys to fail validation")
		}
	})

	t.Run("rejects secret with invalid public key format", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "test-ns",
			},
			Data: map[string][]byte{
				"authorized_keys": []byte("not-a-valid-ssh-key"),
			},
		}

		err := ValidateSSHSecret(secret, logger)
		if err == nil {
			t.Error("Expected secret with invalid authorized_keys to fail validation")
		}
	})

	t.Run("validates secret with ssh-rsa public key", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "test-ns",
			},
			Data: map[string][]byte{
				"authorized_keys": []byte("ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCgdRkm8/liQKXLAUVsC9ohk+TJk0/lJIy/7jDK0VoZtK9mnEDCxtsk1swu9n4q5yzg6kDevOSvdy+RmBddMc9P4QMbvmXLkXwq7JXIDAGuS80xwXDl1TtvwT760uuhmSD9jBNYiD26+p+YAvutFcr3XDgv4JFuLs7oSgSnRxOd2JOdx8n/XMzWBsADVNErfZ1+AqF37JI6wxn2eVCgbSJRQguA2uk2V1DWvhaptGXGYyUjjuwcVh525gGwqEYaaokzkTha8WsjJRE1Edlj2j34C3j+5p/vIASbYy0ah20j1Qi1aYplOkOIZcT3t+NFR9cqvwN//bhBQJrYXGS9c781 test@example.com"),
			},
		}

		err := ValidateSSHSecret(secret, logger)
		if err != nil {
			t.Errorf("Expected secret with valid ssh-rsa authorized_keys to pass validation, got error: %v", err)
		}
	})
}

func TestValidateFileBrowserSecret(t *testing.T) {
	logger := logr.Discard()

	t.Run("validates secret with valid password", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "test-ns",
			},
			Data: map[string][]byte{
				"password": []byte("this-is-a-secure-password"),
			},
		}

		err := ValidateFileBrowserSecret(secret, logger)
		if err != nil {
			t.Errorf("Expected valid secret to pass validation, got error: %v", err)
		}
	})

	t.Run("validates secret with password and username", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "test-ns",
			},
			Data: map[string][]byte{
				"password": []byte("this-is-a-secure-password"),
				"username": []byte("testuser"),
			},
		}

		err := ValidateFileBrowserSecret(secret, logger)
		if err != nil {
			t.Errorf("Expected valid secret to pass validation, got error: %v", err)
		}
	})

	t.Run("rejects secret with nil data", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "test-ns",
			},
			Data: nil,
		}

		err := ValidateFileBrowserSecret(secret, logger)
		if err == nil {
			t.Error("Expected secret with nil data to fail validation")
		}
	})

	t.Run("rejects secret without password", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "test-ns",
			},
			Data: map[string][]byte{
				"username": []byte("testuser"),
			},
		}

		err := ValidateFileBrowserSecret(secret, logger)
		if err == nil {
			t.Error("Expected secret without password to fail validation")
		}
	})

	t.Run("rejects secret with empty password", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "test-ns",
			},
			Data: map[string][]byte{
				"password": []byte(""),
			},
		}

		err := ValidateFileBrowserSecret(secret, logger)
		if err == nil {
			t.Error("Expected secret with empty password to fail validation")
		}
	})

	t.Run("rejects secret with password shorter than minimum length", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "test-ns",
			},
			Data: map[string][]byte{
				"password": []byte("short"), // 5 characters, less than DefaultMinimumPasswordLength (12)
			},
		}

		err := ValidateFileBrowserSecret(secret, logger)
		if err == nil {
			t.Error("Expected secret with short password to fail validation")
		}
	})

	t.Run("accepts password exactly at minimum length", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "test-ns",
			},
			Data: map[string][]byte{
				"password": []byte("exactly12chr"), // Exactly 12 characters
			},
		}

		err := ValidateFileBrowserSecret(secret, logger)
		if err != nil {
			t.Errorf("Expected password at minimum length to pass validation, got error: %v", err)
		}
	})
}

func TestHasVMFRLabel(t *testing.T) {
	t.Run("returns true when label exists", func(t *testing.T) {
		obj := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
				Labels: map[string]string{
					constant.VMFROriginUUIDLabel: "test-uuid-123",
				},
			},
		}

		result := HasVMFRLabel(obj)
		if !result {
			t.Error("Expected true when VMFROriginUUIDLabel exists")
		}
	})

	t.Run("returns false when label does not exist", func(t *testing.T) {
		obj := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
				Labels: map[string]string{
					"other-label": "value",
				},
			},
		}

		result := HasVMFRLabel(obj)
		if result {
			t.Error("Expected false when VMFROriginUUIDLabel does not exist")
		}
	})

	t.Run("returns false when labels are nil", func(t *testing.T) {
		obj := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
			},
		}

		result := HasVMFRLabel(obj)
		if result {
			t.Error("Expected false when labels are nil")
		}
	})

	t.Run("returns true with empty label value", func(t *testing.T) {
		obj := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
				Labels: map[string]string{
					constant.VMFROriginUUIDLabel: "",
				},
			},
		}

		result := HasVMFRLabel(obj)
		if !result {
			t.Error("Expected true when VMFROriginUUIDLabel exists even with empty value")
		}
	})
}

func TestFormatSizeHumanReadable(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "1 byte",
			input:    "1",
			expected: "1",
		},
		{
			name:     "1 KiB",
			input:    "1024",
			expected: "1Ki",
		},
		{
			name:     "1 MiB",
			input:    "1048576",
			expected: "1Mi",
		},
		{
			name:     "1 GiB",
			input:    "1073741824",
			expected: "1Gi",
		},
		{
			name:     "5 GiB",
			input:    "5368709120",
			expected: "5Gi",
		},
		{
			name:     "30 GiB",
			input:    "32212254720",
			expected: "30Gi",
		},
		{
			name:     "1 TiB",
			input:    "1099511627776",
			expected: "1Ti",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quantity := resource.MustParse(tt.input)
			result := FormatSizeHumanReadable(quantity)

			if result != tt.expected {
				t.Errorf("FormatSizeHumanReadable(%s) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMin(t *testing.T) {
	tests := []struct {
		name     string
		a        int
		b        int
		expected int
	}{
		{
			name:     "a less than b",
			a:        5,
			b:        10,
			expected: 5,
		},
		{
			name:     "b less than a",
			a:        10,
			b:        5,
			expected: 5,
		},
		{
			name:     "equal values",
			a:        7,
			b:        7,
			expected: 7,
		},
		{
			name:     "negative values",
			a:        -5,
			b:        -10,
			expected: -10,
		},
		{
			name:     "zero and positive",
			a:        0,
			b:        5,
			expected: 0,
		},
		{
			name:     "zero and negative",
			a:        0,
			b:        -5,
			expected: -5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := min(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("min(%d, %d) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}
