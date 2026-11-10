GO ?= go
DOCKER ?= docker
IMAGE ?= runloom-workflows:local
WORKER_IMAGE ?= runloom-worker:local
export GOWORK := off
.PHONY: build test race vet run docker-build docker-build-api docker-build-worker
build:
	@mkdir -p bin
	$(GO) build -mod=vendor -o bin/runloom-workflows ./cmd/workflows
	$(GO) build -mod=vendor -o bin/runloom-worker ./cmd/worker
	$(GO) build -mod=vendor -o bin/runloom-code ./cmd/code
test:
	$(GO) test -mod=vendor ./...
race:
	$(GO) test -mod=vendor -race ./...
vet:
	$(GO) vet -mod=vendor ./...
run:
	$(GO) run -mod=vendor ./cmd/workflows
docker-build: docker-build-api docker-build-worker
docker-build-api:
	$(DOCKER) build --tag $(IMAGE) .
docker-build-worker:
	$(DOCKER) build --file Dockerfile.worker --tag $(WORKER_IMAGE) .
