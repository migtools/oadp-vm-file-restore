# OADP VM File Restore File Browser Container

Web-based file browser container for accessing OADP VM backup files through a modern HTTPS interface.

## Features

- ✅ Web-based file browsing with modern UI
- ✅ File download (single files and directories as zip)
- ✅ File preview (images, videos, text files, PDFs)
- ✅ Search and filtering
- ✅ HTTPS/TLS enabled by default using OpenShift service CA
- ✅ Non-root container (UID 1000)
- ✅ Multi-architecture support (amd64, arm64)

## Container Image

**Pre-built image:** `quay.io/konveyor/oadp-vmfr-access-filebrowser:latest`

Built from: https://github.com/migtools/filebrowser

## Quick Start

```bash
# Create namespace
oc apply -f sample-deployment/1_namespace.yaml

# Create credentials secret
oc apply -f sample-deployment/2_secret.yaml

# Create PVC
oc apply -f sample-deployment/3_pvc.yaml

# Deploy pod and service
oc apply -f sample-deployment/4_serving_pod.yaml

# Create route
oc apply -f sample-deployment/5_route.yaml

# Get route URL
oc get route -n vmfr-pod-test file-serving-http -o jsonpath='{.spec.host}'
```

For default `oadp` user login with password only `oadppassword` (default values from sample secret).

**IMPORTANT:** Change the password in production! Edit `2_secret.yaml` or create a new secret:

```bash
oc create secret generic filebrowser-credentials -n vmfr-pod-test \
  --from-literal=username='oadp' \
  --from-literal=password='your-strong-password-min-12-chars'
```

## Configuration

### Authentication

Credentials are stored in a Kubernetes Secret (`filebrowser-credentials`):
- `username` - Username for login (optional, defaults to `oadp`)
- `password` - Password (required, minimum 12 characters)

When username is `oadp` or not provided, the login form pre-fills with "oadp" for convenience.

### Branding

Set via environment variable:
- `FB_BRANDING_NAME` - Application title (default: `OADP VM File Restore Browser`)

### TLS/SSL

Automatically configured using OpenShift service serving certificates:
- Service annotation `service.beta.openshift.io/serving-cert-secret-name: filebrowser-tls` triggers certificate generation
- Container serves HTTPS on port 8443
- Route uses `reencrypt` termination for end-to-end encryption

### Permissions

The deployment automatically creates a read-only user with locked settings:
- ✅ View and download files
- ❌ Create, delete, modify, rename, share, or execute

All permissions are locked at container startup for security.

## Deployment

Complete sample deployment is available in `sample-deployment/`:

- `1_namespace.yaml` - Namespace with privileged pod security
- `2_secret.yaml` - Credentials (username/password)
- `3_pvc.yaml` - PVC for backup data
- `4_serving_pod.yaml` - Pod with init container, filebrowser, and service
- `5_route.yaml` - OpenShift Route with reencrypt TLS

### Required Volumes

- **Backup data:** `/srv/restores` (mounted read-only from PVC)
- **Database:** `/database` (emptyDir for user/settings)
- **Tmp:** `/tmp` (emptyDir for read-only root filesystem)
- **TLS certs:** `/etc/filebrowser-tls` (auto-generated secret)
- **Credentials:** `/etc/filebrowser-credentials` (user-provided secret)

### Security Context

```yaml
securityContext:
  runAsUser: 1000
  runAsNonRoot: true
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  capabilities:
    drop: [ALL]
```

## Security

**Best Practices:**
- Always use a strong password (minimum 12 characters)
- Store credentials in Kubernetes Secrets
- Mount backup volumes as `readOnly: true`
- Use default HTTPS/TLS configuration
- Review user permissions in logs after first startup

## Comparison with SSHD

| Feature | File Browser | SSHD |
|---------|-------------|------|
| Interface | Web UI | Command-line |
| Authentication | Username/password | SSH keys |
| Encryption | HTTPS (8443) | SSH (2222) |
| Best For | Interactive browsing | Automation, bulk downloads |
| Certificates | OpenShift service CA | N/A |

Both can be deployed together in the same pod for multiple access methods.

