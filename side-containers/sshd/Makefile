# Makefile for building OADP VM File Restore SSHD container

# Default image name and tag
IMAGE_REGISTRY ?= quay.io
IMAGE_ORG ?= migtools
IMAGE_NAME ?= oadp-vmfr-sshd
IMAGE_TAG ?= latest
IMAGE ?= $(IMAGE_REGISTRY)/$(IMAGE_ORG)/$(IMAGE_NAME):$(IMAGE_TAG)

# Container tool (podman or docker)
# Auto-detect: prefer podman if available, fallback to docker
CONTAINER_TOOL ?= $(shell command -v podman 2>/dev/null || echo docker)

# Platforms for multi-arch builds
PLATFORMS ?= linux/amd64,linux/arm64

# Buildx builder name
BUILDER_NAME ?= oadp-multiarch-builder

.PHONY: help
help: ## Display this help
	@echo "Available targets:"
	@awk 'BEGIN {FS = ":.*##"; printf "\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

.PHONY: build
build: ## Build the container image
	@echo "Building image $(IMAGE) using $(CONTAINER_TOOL)..."
	$(CONTAINER_TOOL) build -t $(IMAGE) -f Containerfile .
	@echo "Image built successfully: $(IMAGE)"

.PHONY: build-podman
build-podman: ## Build the container image using podman
	@echo "Building image $(IMAGE) using podman..."
	podman build -t $(IMAGE) -f Containerfile .
	@echo "Image built successfully: $(IMAGE)"

.PHONY: build-docker
build-docker: ## Build the container image using docker
	@echo "Building image $(IMAGE) using docker..."
	docker build -t $(IMAGE) -f Containerfile .
	@echo "Image built successfully: $(IMAGE)"

.PHONY: push
push: ## Push the container image to registry
	@echo "Pushing image $(IMAGE) using $(CONTAINER_TOOL)..."
	$(CONTAINER_TOOL) push $(IMAGE)
	@echo "Image pushed successfully: $(IMAGE)"

.PHONY: push-podman
push-podman: ## Push the container image using podman
	@echo "Pushing image $(IMAGE) using podman..."
	podman push $(IMAGE)
	@echo "Image pushed successfully: $(IMAGE)"

.PHONY: push-docker
push-docker: ## Push the container image using docker
	@echo "Pushing image $(IMAGE) using docker..."
	docker push $(IMAGE)
	@echo "Image pushed successfully: $(IMAGE)"

.PHONY: build-push
build-push: build push ## Build and push the container image

.PHONY: clean
clean: ## Clean up local images
	@echo "Cleaning up local images..."
	$(CONTAINER_TOOL) rmi $(IMAGE) 2>/dev/null || true

.PHONY: login-quay
login-quay: ## Login to quay.io registry
	@echo "Logging into quay.io..."
	$(CONTAINER_TOOL) login quay.io

.PHONY: info
info: ## Show build information
	@echo "Container Tool: $(CONTAINER_TOOL)"
	@echo "Image: $(IMAGE)"
	@echo "Registry: $(IMAGE_REGISTRY)"
	@echo "Organization: $(IMAGE_ORG)"
	@echo "Name: $(IMAGE_NAME)"
	@echo "Tag: $(IMAGE_TAG)"
	@echo "Platforms: $(PLATFORMS)"
	@echo "Builder Name: $(BUILDER_NAME)"

##@ Multi-Architecture Builds

.PHONY: buildx-create
buildx-create: ## Create buildx builder for multi-arch builds (docker only)
	@echo "Creating buildx builder: $(BUILDER_NAME)..."
	@if [ "$(CONTAINER_TOOL)" = "docker" ]; then \
		docker buildx create --name $(BUILDER_NAME) --use --bootstrap 2>/dev/null || \
		docker buildx use $(BUILDER_NAME); \
		echo "Buildx builder created/activated: $(BUILDER_NAME)"; \
	else \
		echo "Note: Podman doesn't require buildx setup"; \
	fi

.PHONY: buildx-remove
buildx-remove: ## Remove buildx builder
	@echo "Removing buildx builder: $(BUILDER_NAME)..."
	@if [ "$(CONTAINER_TOOL)" = "docker" ]; then \
		docker buildx rm $(BUILDER_NAME) || true; \
	fi

.PHONY: build-multiarch
build-multiarch: ## Build multi-architecture image (amd64 and arm64)
	@echo "Building multi-arch image $(IMAGE) for platforms: $(PLATFORMS)"
	@if [ "$(CONTAINER_TOOL)" = "podman" ]; then \
		echo "Building with podman..."; \
		podman build \
			--platform $(PLATFORMS) \
			--manifest $(IMAGE) \
			-f Containerfile .; \
	else \
		echo "Building with docker buildx..."; \
		docker buildx build \
			--platform $(PLATFORMS) \
			-t $(IMAGE) \
			-f Containerfile \
			--load \
			.; \
	fi
	@echo "Multi-arch image built successfully: $(IMAGE)"

.PHONY: build-push-multiarch
build-push-multiarch: ## Build and push multi-architecture image
	@echo "Building and pushing multi-arch image $(IMAGE) for platforms: $(PLATFORMS)"
	@if [ "$(CONTAINER_TOOL)" = "podman" ]; then \
		echo "Building with podman..."; \
		podman build \
			--platform $(PLATFORMS) \
			--manifest $(IMAGE) \
			-f Containerfile .; \
		echo "Pushing manifest..."; \
		podman manifest push $(IMAGE) $(IMAGE); \
	else \
		echo "Building with docker buildx..."; \
		docker buildx build \
			--platform $(PLATFORMS) \
			-t $(IMAGE) \
			-f Containerfile \
			--push \
			.; \
	fi
	@echo "Multi-arch image built and pushed successfully: $(IMAGE)"

.PHONY: build-amd64
build-amd64: ## Build image for amd64/x86_64 only
	@echo "Building amd64 image $(IMAGE)..."
	$(CONTAINER_TOOL) build \
		--platform linux/amd64 \
		-t $(IMAGE)-amd64 \
		-f Containerfile .
	@echo "AMD64 image built: $(IMAGE)-amd64"

.PHONY: build-arm64
build-arm64: ## Build image for arm64/aarch64 only
	@echo "Building arm64 image $(IMAGE)..."
	$(CONTAINER_TOOL) build \
		--platform linux/arm64 \
		-t $(IMAGE)-arm64 \
		-f Containerfile .
	@echo "ARM64 image built: $(IMAGE)-arm64"

.PHONY: inspect-manifest
inspect-manifest: ## Inspect the multi-arch manifest
	@echo "Inspecting manifest for $(IMAGE)..."
	@if [ "$(CONTAINER_TOOL)" = "podman" ]; then \
		podman manifest inspect $(IMAGE); \
	else \
		docker buildx imagetools inspect $(IMAGE); \
	fi

# Example usage with custom values:
# make build IMAGE_TAG=v1.0.0
# make build-push IMAGE_ORG=myorg IMAGE_TAG=dev
# make build CONTAINER_TOOL=docker
# make build-multiarch PLATFORMS=linux/amd64,linux/arm64
# make build-push-multiarch IMAGE_TAG=v1.0.0
