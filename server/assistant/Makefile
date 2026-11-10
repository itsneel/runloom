GO ?= go
DOCKER ?= docker
IMAGE ?= runloom-assistant:local
export GOWORK := off
.PHONY: build test run race vet docker-build
build:
	$(GO) build -mod=vendor -o bin/runloom-assistant ./cmd/assistant
test:
	$(GO) test -mod=vendor ./...
run:
	$(GO) run -mod=vendor ./cmd/assistant
race:
	$(GO) test -mod=vendor -race ./...
vet:
	$(GO) vet -mod=vendor ./...
docker-build:
	$(DOCKER) build --tag $(IMAGE) .
