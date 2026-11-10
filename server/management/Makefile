GO ?= go
DOCKER ?= docker
IMAGE ?= runloom-management:local
export GOWORK := off
.PHONY: build test race vet run docker-build
build:
	@mkdir -p bin
	$(GO) build -mod=vendor -o bin/runloom-management ./cmd/management
test:
	$(GO) test -mod=vendor ./...
race:
	$(GO) test -mod=vendor -race ./...
vet:
	$(GO) vet -mod=vendor ./...
run:
	$(GO) run -mod=vendor ./cmd/management
docker-build:
	$(DOCKER) build --tag $(IMAGE) .
