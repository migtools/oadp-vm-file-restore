FROM brew.registry.redhat.io/rh-osbs/openshift-golang-builder:rhel_9_golang_1.25 AS builder
WORKDIR /workspace
COPY containers/side-containers/sshd/oadp-sshd.go .
ENV GOEXPERIMENT strictfipsruntime
RUN CGO_ENABLED=1 GOOS=linux go build -tags strictfipsruntime -ldflags="-s -w" -o oadp-sshd oadp-sshd.go

FROM registry.redhat.io/ubi9/ubi-minimal:latest

COPY containers/side-containers/sshd/LICENSE /licenses/LICENSE

# Install required packages
RUN microdnf install -y \
    openssh-server \
    openssh-clients \
    shadow-utils \
    passwd \
    rsync \
    bash \
    findutils \
    && microdnf clean all

# Create chroot directory structure at /oadp
RUN mkdir -p /oadp/bin \
    && mkdir -p /oadp/lib \
    && mkdir -p /oadp/lib64 \
    && mkdir -p /oadp/usr/lib \
    && mkdir -p /oadp/usr/lib64 \
    && mkdir -p /oadp/usr/libexec/openssh \
    && mkdir -p /oadp/dev \
    && mkdir -p /oadp/.ssh

# Copy binaries into chroot
RUN cp /usr/bin/rsync /oadp/bin/ \
    && cp /usr/bin/scp /oadp/bin/ \
    && cp /bin/bash /oadp/bin/ \
    && cp /usr/libexec/openssh/sftp-server /oadp/usr/libexec/openssh/

# Copy library dependencies for all architectures (x86_64 and aarch64)
RUN for binary in /usr/bin/rsync /usr/bin/scp /bin/bash /usr/libexec/openssh/sftp-server; do \
        ldd "$binary" 2>/dev/null | grep "=>" | awk '{print $3}' | while read lib; do \
            if [ -f "$lib" ]; then \
                case "$lib" in \
                    /lib64/*) mkdir -p /oadp/lib64 && cp -L "$lib" /oadp/lib64/ 2>/dev/null || true ;; \
                    /lib/*) mkdir -p /oadp/lib && cp -L "$lib" /oadp/lib/ 2>/dev/null || true ;; \
                    /usr/lib64/*) mkdir -p /oadp/usr/lib64 && cp -L "$lib" /oadp/usr/lib64/ 2>/dev/null || true ;; \
                    /usr/lib/*) mkdir -p /oadp/usr/lib && cp -L "$lib" /oadp/usr/lib/ 2>/dev/null || true ;; \
                esac; \
            fi; \
        done; \
        ldd "$binary" 2>/dev/null | grep -E "ld-linux|ld.so" | awk '{print $1}' | while read linker; do \
            if [ -f "$linker" ]; then \
                case "$linker" in \
                    /lib64/*) mkdir -p /oadp/lib64 && cp -L "$linker" /oadp/lib64/ 2>/dev/null || true ;; \
                    /lib/*) mkdir -p /oadp/lib && cp -L "$linker" /oadp/lib/ 2>/dev/null || true ;; \
                esac; \
            fi; \
        done; \
    done

# Copy compiled Go binary from builder stage
COPY --from=builder /workspace/oadp-sshd /oadp/bin/oadp-sshd

# Copy entrypoint script
COPY containers/side-containers/sshd/entrypoint.sh /entrypoint.sh

# Make binaries executable
RUN chmod 711 /oadp/bin/oadp-sshd \
    && chmod +x /entrypoint.sh \
    && chmod 755 /oadp/bin/rsync \
    && chmod 755 /oadp/bin/scp \
    && chmod 755 /oadp/bin/bash \
    && chmod 755 /oadp/usr/libexec/openssh/sftp-server

# Set proper ownership for chroot directory
RUN chown root:root /oadp \
    && chmod 755 /oadp

# Create a template directory with base /etc files
RUN mkdir -p /etc-template \
    && cp -a /etc/passwd /etc/group /etc/shadow /etc/gshadow /etc/nsswitch.conf /etc-template/ \
    && cp -a /etc/pam.d /etc-template/

EXPOSE 2222

USER root

ENTRYPOINT ["/entrypoint.sh"]

LABEL description="OpenShift API for Data Protection - SSHD for VM File Restore"
LABEL io.k8s.description="OpenShift API for Data Protection - SSHD for VM File Restore"
LABEL io.k8s.display-name="OADP VMFR Access SSHD"
LABEL io.openshift.tags="migration"
LABEL summary="OpenShift API for Data Protection - SSHD for VM File Restore"
LABEL com.redhat.component="oadp-vmfr-access-sshd-container"
