GO ?= go
DOCKER ?= docker
IMAGE ?= runloom-gateway:local
export GOWORK := off
.PHONY: build test race vet run docker-build
build:
	@mkdir -p bin
	$(GO) build -mod=vendor -o bin/runloom-gateway ./cmd/gateway
test:
	$(GO) test -mod=vendor ./...
race:
	$(GO) test -mod=vendor -race ./...
vet:
	$(GO) vet -mod=vendor ./...
run:
	$(GO) run -mod=vendor ./cmd/gateway
docker-build:
	$(DOCKER) build --tag $(IMAGE) .
