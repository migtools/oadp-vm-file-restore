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
	"k8s.io/utils/ptr"
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
// Uses the provided username or defaults to "admin".
func GenerateFileBrowserCredentials(username string, logger logr.Logger) (*FileBrowserCredentials, error) {
	// Use default username if not provided
	if username == "" {
		username = "admin"
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

// CreateSSHCredentialsSecret creates a Kubernetes Secret containing SSH credentials.
// The Secret will contain:
// - username: SSH username
// - privateKey: SSH private key in PEM format
// - publicKey: SSH public key in authorized_keys format
// The Secret will have labels indicating it's managed by VMFR and owned by the specified VMFR resource.
func CreateSSHCredentialsSecret(
	name string,
	namespace string,
	username string,
	keyPair *SSHKeyPair,
	vmfrName string,
	vmfrNamespace string,
	vmfrUID apitypes.UID,
	logger logr.Logger,
) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				constant.ManagedByLabel:                constant.ManagedByLabelValue,
				"oadp.openshift.io/vm-file-restore":    vmfrName,
				"oadp.openshift.io/vm-file-restore-ns": vmfrNamespace,
				"oadp.openshift.io/credential-type":    "ssh",
			},
			Annotations: map[string]string{
				"oadp.openshift.io/generated-by": "oadp-vm-file-restore-controller",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "oadp.openshift.io/v1alpha1",
					Kind:               "VirtualMachineFileRestore",
					Name:               vmfrName,
					UID:                vmfrUID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"username":   username,
			"privateKey": keyPair.PrivateKey,
			"publicKey":  keyPair.PublicKey,
		},
	}

	logger.V(1).Info("Created SSH credentials secret",
		"secretName", name,
		"secretNamespace", namespace,
		"username", username)

	return secret
}

// CreateFileBrowserCredentialsSecret creates a Kubernetes Secret containing FileBrowser credentials.
// The Secret will contain:
// - username: FileBrowser username
// - password: FileBrowser password
// The Secret will have labels indicating it's managed by VMFR and owned by the specified VMFR resource.
func CreateFileBrowserCredentialsSecret(
	name string,
	namespace string,
	credentials *FileBrowserCredentials,
	vmfrName string,
	vmfrNamespace string,
	vmfrUID apitypes.UID,
	logger logr.Logger,
) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				constant.ManagedByLabel:                constant.ManagedByLabelValue,
				"oadp.openshift.io/vm-file-restore":    vmfrName,
				"oadp.openshift.io/vm-file-restore-ns": vmfrNamespace,
				"oadp.openshift.io/credential-type":    "filebrowser",
			},
			Annotations: map[string]string{
				"oadp.openshift.io/generated-by": "oadp-vm-file-restore-controller",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "oadp.openshift.io/v1alpha1",
					Kind:               "VirtualMachineFileRestore",
					Name:               vmfrName,
					UID:                vmfrUID,
					Controller:         ptr.To(true),
					BlockOwnerDeletion: ptr.To(true),
				},
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"username": credentials.Username,
			"password": credentials.Password,
		},
	}

	logger.V(1).Info("Created FileBrowser credentials secret",
		"secretName", name,
		"secretNamespace", namespace,
		"username", credentials.Username)

	return secret
}
