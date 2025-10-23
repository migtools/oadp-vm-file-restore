// Package function provides common utility functions for the OADP VM file restore controller,
// including metadata validation and logging helpers.
package function

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-logr/logr"
	"github.com/migtools/oadp-vm-file-restore/internal/common/constant"
	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// CheckVeleroRestoreMetadata return true if Velero Restore object has required VMFR origin labels
func CheckVeleroRestoreMetadata(obj client.Object) bool {
	objLabels := obj.GetLabels()
	_, exists := objLabels[constant.VMFROriginUUIDLabel]
	return exists
}

// GetLogger return a logger from input ctx, with additional key/value pairs being
// input key and input obj name and namespace
func GetLogger(ctx context.Context, obj client.Object, key string) logr.Logger {
	return log.FromContext(ctx).WithValues(key, apitypes.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()})
}

// FormatSizeHumanReadable converts a resource.Quantity to human-readable storage format.
// It ensures consistent formatting using binary units (Ki, Mi, Gi, Ti) which are standard for storage
func FormatSizeHumanReadable(quantity resource.Quantity) string {
	// Convert to bytes to ensure we start with a consistent base
	bytes := quantity.Value()

	// Create a new quantity from the byte value and format it with binary units
	newQuantity := resource.NewQuantity(bytes, resource.BinarySI)

	// The String() method will automatically choose the appropriate unit
	// e.g., 5368709120 bytes -> 5Gi, 32212254720 bytes -> 30Gi
	return newQuantity.String()
}

// GenerateTemporaryVMFRNamespaceName generates a unique temporary namespace name.
// Format: [prefix-]<vm-namespace>-<vm-name>-<suffix>
// - prefix: optional string to prepend (can be empty)
// - vmNamespace, vmName: names of the VM
// - uid: UID string for uniqueness
func GenerateTemporaryVMFRNamespaceName(prefix, vmNamespace, vmName, uid string, logger logr.Logger) string {
	var nameParts []string

	// Optional prefix
	if prefix != "" {
		nameParts = append(nameParts, prefix)
	}

	// Add VM namespace and name
	nameParts = append(nameParts, vmNamespace, vmName)

	// Ensure suffix is always 8 chars (pad or truncate)
	var suffix string
	if len(uid) >= 8 {
		suffix = uid[:8]
	} else {
		// Pad short UID with zeros for stability
		suffix = uid + strings.Repeat("0", 8-len(uid))
	}
	nameParts = append(nameParts, suffix)

	// Join with hyphens
	namespaceName := strings.Join(nameParts, "-")

	// Ensure max 63 chars (DNS-1123)
	if len(namespaceName) > 63 {
		maxPrefixLen := 63 - len(suffix) - 1 // -1 for hyphen
		if maxPrefixLen > 0 && maxPrefixLen < len(namespaceName) {
			namespaceName = namespaceName[:maxPrefixLen] + "-" + suffix
		} else {
			namespaceName = suffix
		}
	}

	// Convert to lowercase
	namespaceName = strings.ToLower(namespaceName)

	// Replace invalid DNS-1123 chars (anything not a-z, 0-9, or '-')
	re := regexp.MustCompile(`[^a-z0-9-]`)
	namespaceName = re.ReplaceAllString(namespaceName, "-")

	// Trim leading/trailing hyphens (illegal in DNS-1123)
	namespaceName = strings.Trim(namespaceName, "-")

	logger.V(1).Info("Generated temporary namespace name",
		"namespace", namespaceName,
		"vmNamespace", vmNamespace,
		"vmName", vmName,
		"prefix", prefix)

	return namespaceName
}

// GenerateVeleroRestorePrefix generates a prefix for Velero Restore generateName field.
// Kubernetes will automatically append a random suffix (5 chars) to ensure uniqueness.
// Format: vmfr-<vmfr-name>-<backup-name>-
// - vmfrName: Name of the VirtualMachineFileRestore resource
// - backupName: Name of the Velero backup
func GenerateVeleroRestorePrefix(vmfrName, backupName string, logger logr.Logger) string {
	// Build restore prefix: vmfr-<name>-<backup>-
	// K8s will append random 5-char suffix like "x7k2j"
	restorePrefix := "vmfr-" + vmfrName + "-" + backupName + "-"

	// Convert to lowercase (DNS-1123 requirement)
	restorePrefix = strings.ToLower(restorePrefix)

	logger.V(1).Info("Generated Velero Restore prefix for generateName",
		"restorePrefix", restorePrefix,
		"vmfrName", vmfrName,
		"backupName", backupName)

	return restorePrefix
}

// SSHKeyPair represents an SSH public/private keypair
type SSHKeyPair struct {
	// PrivateKey is the SSH private key in OpenSSH PEM format
	PrivateKey string
	// PublicKey is the SSH public key in OpenSSH authorized_keys format
	PublicKey string
}

// FileBrowserCredentials represents username/password credentials for FileBrowser
type FileBrowserCredentials struct {
	// Username for FileBrowser access
	Username string
	// Password for FileBrowser access
	Password string
}

// GenerateSSHKeyPair generates a new ED25519 SSH keypair.
// Returns an SSHKeyPair with PrivateKey in OpenSSH PEM format and PublicKey in authorized_keys format.
// ED25519 is chosen for its security, small key size, and fast operations.
func GenerateSSHKeyPair(logger logr.Logger) (*SSHKeyPair, error) {
	// Generate ED25519 keypair using crypto/rand for cryptographically secure randomness
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		logger.Error(err, "Failed to generate ED25519 keypair")
		return nil, fmt.Errorf("failed to generate ED25519 keypair: %w", err)
	}

	// Marshal private key to OpenSSH PEM format
	// This uses the modern OpenSSH private key format (not PKCS#8)
	privateKeyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		logger.Error(err, "Failed to marshal private key to PKCS8")
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	// Create PEM block for private key with OpenSSH-compatible type
	privateKeyPEM := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privateKeyBytes,
	}
	privateKeyStr := string(pem.EncodeToMemory(privateKeyPEM))

	// Convert ED25519 public key to SSH public key format
	sshPublicKey, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		logger.Error(err, "Failed to convert public key to SSH format")
		return nil, fmt.Errorf("failed to convert public key to SSH format: %w", err)
	}

	// Encode public key in OpenSSH authorized_keys format
	// Format: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... [optional comment]
	publicKeyStr := string(ssh.MarshalAuthorizedKey(sshPublicKey))

	logger.V(1).Info("Successfully generated SSH ED25519 keypair",
		"publicKeyPrefix", publicKeyStr[:min(50, len(publicKeyStr))])

	return &SSHKeyPair{
		PrivateKey: privateKeyStr,
		PublicKey:  strings.TrimSpace(publicKeyStr), // Remove trailing newline
	}, nil
}

// GenerateFileBrowserCredentials generates random FileBrowser credentials.
// The password is a cryptographically secure random string (32 bytes, base64 encoded).
// Uses the provided username or defaults to constant.DefaultFileBrowserUsername ("oadp").
func GenerateFileBrowserCredentials(username string, logger logr.Logger) (*FileBrowserCredentials, error) {
	// Use default username if not provided
	if username == "" {
		username = constant.DefaultFileBrowserUsername
	}

	// Generate 32 bytes of cryptographically secure random data for password
	// Base64 encoding will result in a 43-44 character password
	passwordBytes := make([]byte, 32)
	if _, err := rand.Read(passwordBytes); err != nil {
		logger.Error(err, "Failed to generate random password bytes")
		return nil, fmt.Errorf("failed to generate random password: %w", err)
	}

	// Encode to base64 for a safe, printable password
	// Using URLEncoding to avoid special characters that might cause issues
	password := base64.URLEncoding.EncodeToString(passwordBytes)

	logger.V(1).Info("Successfully generated FileBrowser credentials",
		"username", username,
		"passwordLength", len(password))

	return &FileBrowserCredentials{
		Username: username,
		Password: password,
	}, nil
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// createCredentialsSecretBase creates a Secret with common VMFR metadata using generateName.
// Kubernetes will automatically append a random suffix to ensure uniqueness.
// The secret can be looked up later using the VMFROriginUUIDLabel and CredentialTypeLabel.
func createCredentialsSecretBase(
	generateNamePrefix string,
	namespace string,
	credentialType string, // constant.CredentialTypeSSH or constant.CredentialTypeFileBrowser
	vmfrName string,
	vmfrNamespace string,
	vmfrUID apitypes.UID,
) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: generateNamePrefix,
			Namespace:    namespace,
			Labels: map[string]string{
				constant.ManagedByLabel:      constant.ManagedByLabelValue,
				constant.VMFROriginUUIDLabel: string(vmfrUID),
				constant.CredentialTypeLabel: credentialType,
			},
			Annotations: map[string]string{
				constant.VMFROriginNameAnnotation:      vmfrName,
				constant.VMFROriginNamespaceAnnotation: vmfrNamespace,
				"oadp.openshift.io/generated-by":       "oadp-vm-file-restore-controller",
			},
			// NOTE: No OwnerReferences - VMFR may be in different namespace than secret
			// (e.g., VMFR in openshift-adp, secret in restore namespace)
			// Cross-namespace owner references are rejected by Kubernetes
			// Use labels + finalizer for cleanup instead
		},
		Type: corev1.SecretTypeOpaque,
	}
}

// CreateSSHCredentialsSecret creates a Kubernetes Secret containing SSH credentials.
// Uses generateName for automatic unique naming - Kubernetes appends a random suffix.
// The Secret will contain:
// - username: SSH username
// - privateKey: SSH private key in PEM format
// - publicKey: SSH public key in authorized_keys format
// The Secret can be found later using VMFROriginUUIDLabel and CredentialTypeLabel.
func CreateSSHCredentialsSecret(
	generateNamePrefix string,
	namespace string,
	username string,
	keyPair *SSHKeyPair,
	vmfrName string,
	vmfrNamespace string,
	vmfrUID apitypes.UID,
	logger logr.Logger,
) *corev1.Secret {
	secret := createCredentialsSecretBase(
		generateNamePrefix,
		namespace,
		constant.CredentialTypeSSH,
		vmfrName,
		vmfrNamespace,
		vmfrUID,
	)

	secret.StringData = map[string]string{
		"username":   username,
		"privateKey": keyPair.PrivateKey,
		"publicKey":  keyPair.PublicKey,
	}

	logger.V(1).Info("Created SSH credentials secret with generateName",
		"generateNamePrefix", generateNamePrefix,
		"namespace", namespace,
		"username", username)

	return secret
}

// CreateFileBrowserCredentialsSecret creates a Kubernetes Secret containing FileBrowser credentials.
// Uses generateName for automatic unique naming - Kubernetes appends a random suffix.
// The Secret will contain:
// - username: FileBrowser username
// - password: FileBrowser password
// The Secret can be found later using VMFROriginUUIDLabel and CredentialTypeLabel.
func CreateFileBrowserCredentialsSecret(
	generateNamePrefix string,
	namespace string,
	credentials *FileBrowserCredentials,
	vmfrName string,
	vmfrNamespace string,
	vmfrUID apitypes.UID,
	logger logr.Logger,
) *corev1.Secret {
	secret := createCredentialsSecretBase(
		generateNamePrefix,
		namespace,
		constant.CredentialTypeFileBrowser,
		vmfrName,
		vmfrNamespace,
		vmfrUID,
	)

	secret.StringData = map[string]string{
		"username": credentials.Username,
		"password": credentials.Password,
	}

	logger.V(1).Info("Created FileBrowser credentials secret with generateName",
		"generateNamePrefix", generateNamePrefix,
		"namespace", namespace,
		"username", credentials.Username)

	return secret
}

// ValidateSSHPublicKey validates an SSH public key format using the crypto/ssh parser.
// This provides robust validation by actually parsing the key rather than simple string checks.
//
// Security policy: Allows modern secure key types. For RSA keys, we allow "ssh-rsa" (the key type
// identifier in authorized_keys format) because:
// 1. All RSA keys in authorized_keys format are labeled "ssh-rsa" regardless of signature algorithm
// 2. Modern OpenSSH (7.2+) automatically negotiates SHA-2 signatures (rsa-sha2-256/512) at runtime
// 3. If ParseAuthorizedKey succeeds on an RSA key, it's already RSA2 (protocol v2), not the deprecated RSA1
//
// Note: "rsa-sha2-256" and "rsa-sha2-512" are signature algorithm names negotiated during authentication,
// not key type identifiers that appear in the public key itself.
//
// Allowed key types:
// - ssh-ed25519 (recommended - most secure)
// - ssh-rsa (RSA keys - modern SSH uses SHA-2 signatures)
// - ecdsa-sha2-nistp256/384/521 (ECDSA variants)
// - FIDO/U2F hardware key variants
func ValidateSSHPublicKey(publicKey []byte) error {
	parsedKey, _, _, _, err := ssh.ParseAuthorizedKey(publicKey)
	if err != nil {
		return fmt.Errorf("invalid SSH public key format: %w", err)
	}

	// Get the key type identifier from the public key
	keyType := parsedKey.Type()

	// Whitelist of allowed key types
	allowedKeyTypes := map[string]bool{
		"ssh-ed25519":                        true, // Ed25519 - most secure, recommended
		"ssh-rsa":                            true, // RSA - modern SSH uses SHA-2 signatures
		"ecdsa-sha2-nistp256":                true, // ECDSA P-256
		"ecdsa-sha2-nistp384":                true, // ECDSA P-384
		"ecdsa-sha2-nistp521":                true, // ECDSA P-521
		"sk-ssh-ed25519@openssh.com":         true, // FIDO/U2F Ed25519
		"sk-ecdsa-sha2-nistp256@openssh.com": true, // FIDO/U2F ECDSA
	}

	if !allowedKeyTypes[keyType] {
		return fmt.Errorf("SSH key type '%s' is not allowed. Allowed types: ssh-ed25519 (recommended), ssh-rsa, ecdsa-sha2-nistp256/384/521, and FIDO variants", keyType)
	}

	return nil
}

// ValidateSSHSecret validates that a Secret contains the required SSH credential fields.
// Required fields:
// - publicKey: SSH public key in authorized_keys format
// Optional fields:
// - username: SSH username (defaults to "oadp" if not provided)
// - privateKey: SSH private key (only for user reference, not used by server)
func ValidateSSHSecret(secret *corev1.Secret, logger logr.Logger) error {
	if secret.Data == nil {
		return fmt.Errorf("secret data is nil")
	}

	// Check for required publicKey field
	publicKey, exists := secret.Data["publicKey"]
	if !exists || len(publicKey) == 0 {
		return fmt.Errorf("secret missing required field 'publicKey'")
	}

	// Validate publicKey format using robust SSH parser
	if err := ValidateSSHPublicKey(publicKey); err != nil {
		return fmt.Errorf("publicKey validation failed: %w", err)
	}

	logger.V(1).Info("SSH secret validation passed",
		"secretName", secret.Name,
		"secretNamespace", secret.Namespace,
		"hasUsername", secret.Data["username"] != nil,
		"hasPrivateKey", secret.Data["privateKey"] != nil)

	return nil
}

// ValidateFileBrowserSecret validates that a Secret contains the required FileBrowser credential fields.
// Required fields:
// - password: FileBrowser password
// Optional fields:
// - username: FileBrowser username (defaults to "oadp" if not provided)
func ValidateFileBrowserSecret(secret *corev1.Secret, logger logr.Logger) error {
	if secret.Data == nil {
		return fmt.Errorf("secret data is nil")
	}

	// Check for required password field
	password, exists := secret.Data["password"]
	if !exists || len(password) == 0 {
		return fmt.Errorf("secret missing required field 'password'")
	}

	// Validate password is not too weak (minimum length enforced)
	if len(password) < constant.DefaultMinimumPasswordLength {
		return fmt.Errorf("password must be at least %d characters long", constant.DefaultMinimumPasswordLength)
	}

	logger.V(1).Info("FileBrowser secret validation passed",
		"secretName", secret.Name,
		"secretNamespace", secret.Namespace,
		"hasUsername", secret.Data["username"] != nil)

	return nil
}
