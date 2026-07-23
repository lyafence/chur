.PHONY: build build-webhook build-init build-keeper fmt lint test check vuln clean \
        docker docker-webhook docker-init docker-keeper release e2e helm-package

APP_NAME    ?= chur
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS     ?= -ldflags="-s -w -X main.version=$(VERSION)"
GOFLAGS     ?=
TARGETOS    ?= linux
TARGETARCH  ?= $(shell go env GOARCH)
E2E_CLUSTER      ?= chur-e2e
E2E_SKIP_CLEANUP ?= false

# Use podman if available, otherwise fall back to docker.
DOCKER := $(shell command -v podman 2>/dev/null || echo docker)

# When using podman, tell Kind to use the podman provider.
ifeq ($(DOCKER),podman)
export KIND_EXPERIMENTAL_PROVIDER = podman
endif

build: build-webhook build-init build-keeper

build-webhook:
	CGO_ENABLED=0 go build $(GOFLAGS) $(LDFLAGS) -o bin/chur-webhook ./cmd/webhook

build-init:
	CGO_ENABLED=0 go build $(GOFLAGS) -tags provider_k8s $(LDFLAGS) -o bin/chur-init ./cmd/init

build-keeper:
	CGO_ENABLED=0 go build $(GOFLAGS) $(LDFLAGS) -o bin/chur-keeper ./cmd/keeper

# Build init without the k8s provider (smaller binary, 12 MB vs 26 MB)
build-init-minimal:
	CGO_ENABLED=0 go build $(GOFLAGS) $(LDFLAGS) -o bin/chur-init ./cmd/init

fmt:
	gofmt -w .

lint:
	golangci-lint run ./...

test:
	go test -race -count=1 -timeout 120s -tags provider_k8s -coverprofile=coverage.out ./...

check: lint test build

vuln:
	govulncheck ./...

clean:
	rm -rf bin/ dist/ release/
	rm -f coverage.out *.log coverage.html

docker-webhook:
	$(DOCKER) build --platform $(TARGETOS)/$(TARGETARCH) \
		--build-arg TARGETOS=$(TARGETOS) \
		--build-arg TARGETARCH=$(TARGETARCH) \
		--build-arg VERSION=$(VERSION) \
		-t $(APP_NAME)-webhook:$(VERSION) -f Dockerfile.webhook .

docker-init:
	$(DOCKER) build --platform $(TARGETOS)/$(TARGETARCH) \
		--build-arg TARGETOS=$(TARGETOS) \
		--build-arg TARGETARCH=$(TARGETARCH) \
		--build-arg VERSION=$(VERSION) \
		--build-arg GOFLAGS="-tags=provider_k8s" \
		-t $(APP_NAME)-init:$(VERSION) -f Dockerfile.init .

docker-keeper:
	$(DOCKER) build --platform $(TARGETOS)/$(TARGETARCH) \
		--build-arg TARGETOS=$(TARGETOS) \
		--build-arg TARGETARCH=$(TARGETARCH) \
		--build-arg VERSION=$(VERSION) \
		-t $(APP_NAME)-keeper:$(VERSION) -f Dockerfile.keeper .

docker: docker-webhook docker-init docker-keeper

release: build
	rm -rf release && mkdir -p release
	tar -czf release/$(APP_NAME)-webhook-$(VERSION).tar.gz bin/chur-webhook LICENSE README.md
	tar -czf release/$(APP_NAME)-init-$(VERSION).tar.gz bin/chur-init LICENSE README.md
	tar -czf release/$(APP_NAME)-keeper-$(VERSION).tar.gz bin/chur-keeper LICENSE README.md
	@echo "Release: release/"

helm-package:
	helm package charts/chur/ --destination dist/ --version "$(VERSION:v%=%)" --app-version "$(VERSION:v%=%)"

e2e: docker
	./hack/e2e.sh "$(VERSION)" "$(E2E_CLUSTER)" "$(E2E_SKIP_CLEANUP)"
