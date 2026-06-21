.PHONY: build build-webhook build-init lint test check clean \
        docker docker-webhook docker-init release e2e

APP_NAME    ?= chur
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS     ?= -ldflags="-s -w -X main.version=$(VERSION)"
GOFLAGS     ?=
E2E_CLUSTER ?= chur-e2e

# Use podman if available, otherwise fall back to docker.
DOCKER := $(shell command -v podman 2>/dev/null || echo docker)

# When using podman, tell Kind to use the podman provider.
ifeq ($(DOCKER),podman)
export KIND_EXPERIMENTAL_PROVIDER = podman
endif

build: build-webhook build-init

build-webhook:
	go build $(GOFLAGS) $(LDFLAGS) -o bin/chur-webhook ./cmd/webhook

build-init:
	go build $(GOFLAGS) $(LDFLAGS) -o bin/chur-init ./cmd/init

lint:
	golangci-lint run ./...

test:
	go test -race -count=1 -timeout 120s ./...

check: lint test build

clean:
	rm -rf bin/ dist/ release/
	rm -f coverage.out *.log

docker-webhook:
	$(DOCKER) build -t $(APP_NAME)-webhook:$(VERSION) -f Dockerfile.webhook .

docker-init:
	$(DOCKER) build -t $(APP_NAME)-init:$(VERSION) -f Dockerfile.init .

docker: docker-webhook docker-init

release: build
	rm -rf release && mkdir -p release
	tar -czf release/$(APP_NAME)-webhook-$(VERSION).tar.gz bin/chur-webhook LICENSE README.md
	tar -czf release/$(APP_NAME)-init-$(VERSION).tar.gz bin/chur-init LICENSE README.md
	@echo "Release: release/"

e2e: docker
	@echo "Checking prerequisites..."
	@command -v kind >/dev/null 2>&1 || { echo "ERROR: kind is required. Install: go install sigs.k8s.io/kind@latest"; exit 1; }
	@command -v kubectl >/dev/null 2>&1 || { echo "ERROR: kubectl is required"; exit 1; }
	@echo "Creating Kind cluster..."
	systemd-run --user --scope -q -p Delegate=yes -- kind create cluster --name $(E2E_CLUSTER) --wait 60s
	@echo "Loading images into Kind..."
ifeq ($(DOCKER),podman)
	rm -f /tmp/chur-$(APP_NAME)-*.tar
	$(DOCKER) save -o /tmp/chur-$(APP_NAME)-webhook.tar localhost/$(APP_NAME)-webhook:$(VERSION)
	$(DOCKER) save -o /tmp/chur-$(APP_NAME)-init.tar localhost/$(APP_NAME)-init:$(VERSION)
	$(DOCKER) cp /tmp/chur-$(APP_NAME)-webhook.tar $(E2E_CLUSTER)-control-plane:/tmp/chur-webhook.tar
	$(DOCKER) cp /tmp/chur-$(APP_NAME)-init.tar $(E2E_CLUSTER)-control-plane:/tmp/chur-init.tar
	$(DOCKER) exec $(E2E_CLUSTER)-control-plane ctr -n k8s.io images import /tmp/chur-webhook.tar
	$(DOCKER) exec $(E2E_CLUSTER)-control-plane ctr -n k8s.io images import /tmp/chur-init.tar
	WEBHOOK_REF=$$($(DOCKER) exec $(E2E_CLUSTER)-control-plane crictl images 2>/dev/null | grep chur-webhook | head -1 | awk '{print $$1":"$$2}'); \
	INIT_REF=$$($(DOCKER) exec $(E2E_CLUSTER)-control-plane crictl images 2>/dev/null | grep chur-init | head -1 | awk '{print $$1":"$$2}'); \
	$(DOCKER) exec $(E2E_CLUSTER)-control-plane ctr -n k8s.io images tag "$$$$WEBHOOK_REF" docker.io/library/chur-webhook:$(VERSION); \
	$(DOCKER) exec $(E2E_CLUSTER)-control-plane ctr -n k8s.io images tag "$$$$INIT_REF" docker.io/library/chur-init:$(VERSION); \
	$(DOCKER) exec $(E2E_CLUSTER)-control-plane ctr -n k8s.io images tag "$$$$INIT_REF" ghcr.io/lyafence/chur-init:latest
else
	kind load docker-image $(APP_NAME)-webhook:$(VERSION) --name $(E2E_CLUSTER)
	kind load docker-image $(APP_NAME)-init:$(VERSION) --name $(E2E_CLUSTER)
endif
	@echo "Running E2E tests..."
	go test -tags e2e -count=1 -timeout=300s -v ./test/e2e/
	@echo "Cleaning up Kind cluster..."
	kind delete cluster --name $(E2E_CLUSTER)
