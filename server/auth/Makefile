GO ?= go
DOCKER ?= docker
IMAGE ?= runloom-auth:local
LOCAL_ADMIN_IMAGE ?= runloom-local-admin:local
export GOWORK := off
.PHONY: build test race vet run docker-build docker-build-api docker-build-local-admin
build:
	@mkdir -p bin
	$(GO) build -mod=vendor -o bin/runloom-auth ./cmd/auth
	$(GO) build -mod=vendor -o bin/runloom-local-admin ./cmd/local-admin
test:
	$(GO) test -mod=vendor ./...
race:
	$(GO) test -mod=vendor -race ./...
vet:
	$(GO) vet -mod=vendor ./...
run:
	$(GO) run -mod=vendor ./cmd/auth
docker-build: docker-build-api docker-build-local-admin
docker-build-api:
	$(DOCKER) build --tag $(IMAGE) .
docker-build-local-admin:
	$(DOCKER) build --file Dockerfile.local-admin --tag $(LOCAL_ADMIN_IMAGE) .
