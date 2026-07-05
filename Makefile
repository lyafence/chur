.PHONY: build build-webhook build-init lint test check clean \
        docker docker-webhook docker-init release e2e helm-package

APP_NAME    ?= chur
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS     ?= -ldflags="-s -w -X main.version=$(VERSION)"
GOFLAGS     ?=
E2E_CLUSTER      ?= chur-e2e
E2E_SKIP_CLEANUP ?= false

# Use podman if available, otherwise fall back to docker.
DOCKER := $(shell command -v podman 2>/dev/null || echo docker)

# When using podman, tell Kind to use the podman provider.
ifeq ($(DOCKER),podman)
export KIND_EXPERIMENTAL_PROVIDER = podman
endif

build: build-webhook build-init

build-webhook:
	CGO_ENABLED=0 go build $(GOFLAGS) $(LDFLAGS) -o bin/chur-webhook ./cmd/webhook

build-init:
	CGO_ENABLED=0 go build $(GOFLAGS) $(LDFLAGS) -o bin/chur-init ./cmd/init

lint:
	golangci-lint run ./...

test:
	go test -race -count=1 -timeout 120s ./...

check: lint test build

clean:
	rm -rf bin/ dist/ release/
	rm -f coverage.out *.log

docker-webhook: build-webhook
	mkdir -p linux/amd64
	cp bin/$(APP_NAME)-webhook linux/amd64/$(APP_NAME)-webhook
	$(DOCKER) build -t $(APP_NAME)-webhook:$(VERSION) -f Dockerfile.webhook .
	rm -rf linux

docker-init: build-init
	mkdir -p linux/amd64
	cp bin/$(APP_NAME)-init linux/amd64/$(APP_NAME)-init
	$(DOCKER) build -t $(APP_NAME)-init:$(VERSION) -f Dockerfile.init .
	rm -rf linux

docker: docker-webhook docker-init

release: build
	rm -rf release && mkdir -p release
	tar -czf release/$(APP_NAME)-webhook-$(VERSION).tar.gz bin/chur-webhook LICENSE README.md
	tar -czf release/$(APP_NAME)-init-$(VERSION).tar.gz bin/chur-init LICENSE README.md
	@echo "Release: release/"

helm-package:
	helm package charts/chur/ --destination dist/ --version "$(VERSION:v%=%)" --app-version "$(VERSION:v%=%)"

e2e: docker
	./hack/e2e.sh "$(VERSION)" "$(E2E_CLUSTER)" "$(E2E_SKIP_CLEANUP)"
