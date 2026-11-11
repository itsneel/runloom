DOCKER ?= docker
GO ?= go
COMPOSE = $(DOCKER) compose --project-directory "$(CURDIR)" -f compose/compose.yaml
BACKEND_IMAGE ?= runloom-backend:local
WORKER_IMAGE ?= runloom-worker:local
LOCAL_ADMIN_IMAGE ?= runloom-local-admin:local
UI_IMAGE ?= runloom-ui:local
WORKFLOWS_UI_IMAGE ?= runloom-workflows-ui:local
MANAGEMENT_UI_IMAGE ?= runloom-management-ui:local
PLATFORM_UI_IMAGE ?= runloom-platform-ui:local
HELM ?= helm
NODE ?= node
KIND ?= kind
KUBECTL ?= kubectl
# Empty database/port overrides preserve an existing local release's settings.
K8S_CLUSTER ?= runloom-local
K8S_NAMESPACE ?= runloom
K8S_RELEASE ?= runloom
K8S_TIMEOUT ?= 10m
K8S_DATABASE ?=
K8S_PORT ?=
K8S_DOCS_PORT ?=
export K8S_DOCS_PORT
K8S_WEBSITE_PORT ?= 4300
K8S_OPEN_BROWSER ?= 1
export K8S_WEBSITE_PORT K8S_OPEN_BROWSER
export K8S_OBSERVABILITY SERVICES_OBSERVABILITY
export HELM KIND KUBECTL K8S_CLUSTER K8S_NAMESPACE K8S_RELEASE K8S_TIMEOUT K8S_DATABASE K8S_PORT K8S_SERVICE_VALUES
KEYCLOAK_URL ?= http://localhost:18081/auth
KEYCLOAK_REALM ?= runloom
RUNLOOM_LOCAL_ENV_FILE ?= deploy/local.env
export RUNLOOM_LOCAL_ENV_FILE DOCKER

# Each Node project owns its formatter; Go uses gofmt directly.
FORMAT_BASE ?= HEAD
FORMAT_GO_PATHS = '*.go' ':!**/vendor/**' ':!**/node_modules/**' ':!**/dist/**' ':!**/*.pb.go' ':!**/*.generated.go'
FORMAT_NODE_PROJECTS = studio/projects/host studio/projects/workflows studio/projects/management studio/projects/platform-admin studio/libraries/platform Website docs-site server/api-docs
.PHONY: format format-check format-go format-node format-go-check format-node-check
format: format-go format-node
format-check: format-go-check format-node-check
format-go format-node format-go-check format-node-check: SHELL := /bin/bash
format-go:
	@set -o pipefail; git diff --name-only --diff-filter=ACMR -z "$(FORMAT_BASE)" -- $(FORMAT_GO_PATHS) | xargs -0 -r gofmt -w
format-node:
	@set -e; for project in $(FORMAT_NODE_PROJECTS); do npm --prefix "$$project" run format; done
format-go-check:
	@set -o pipefail; git diff --name-only --diff-filter=ACMR -z "$(FORMAT_BASE)" -- $(FORMAT_GO_PATHS) | xargs -0 -r gofmt -d | diff -u /dev/null -
format-node-check:
	@set -e; for project in $(FORMAT_NODE_PROJECTS); do npm --prefix "$$project" run format:check; done

.PHONY: zip
zip: SHELL := /bin/bash
zip:
	@mkdir -p dist
	@set -e -o pipefail; next=1; \
	for file in dist/runloom-source-[0-9]*.zip; do \
		number=$${file#dist/runloom-source-}; number=$${number%.zip}; \
		[[ $$number =~ ^[0-9]+$$ ]] || continue; \
		if (( 10#$$number >= next )); then next=$$((10#$$number + 1)); fi; \
	done; \
	archive=$$(printf 'dist/runloom-source-%03d.zip' "$$next"); \
	set -o noclobber; \
	git ls-files --cached --others --exclude-standard | zip -q -y -@ - > "$$archive"; \
	echo "Created $$archive"

.PHONY: local-up local-admin local-admin-reset local-down local-config test-local-admin test-local-admin-live
local-up: local-services-up

.PHONY: local-services-up local-services-config local-services-down test-auth-sdk test-services-live
SERVICES_DATABASE ?= mongodb
export SERVICES_DATABASE
local-services-up:
	$(NODE) scripts/local-services.mjs up
local-services-config:
	$(NODE) scripts/local-services.mjs config
local-services-down:
	$(NODE) scripts/local-services.mjs down
test-auth-sdk:
	cd sdk/go-auth && go test -race ./...
.PHONY: test-observability-sdk test-observability-live
test-observability-sdk:
	cd sdk/go-observability && go test -race ./...
test-observability-live:
	$(NODE) scripts/test-observability.mjs
test-services-live:
	$(NODE) scripts/test-services.mjs
.PHONY: test-security-live
test-security-live:
	$(MAKE) test-auth-sdk
	cd server/workflows && go test -race ./internal/protection ./internal/api
	$(MAKE) local-services-up SERVICES_DATABASE=mongodb
	$(MAKE) test-services-live SERVICES_DATABASE=mongodb
	$(MAKE) local-services-up SERVICES_DATABASE=postgres
	$(MAKE) test-services-live SERVICES_DATABASE=postgres
local-admin:
	$(NODE) scripts/local-services.mjs admin
local-admin-reset:
	$(NODE) scripts/local-services.mjs reset
local-down: local-services-down
local-config:
	$(NODE) scripts/local-services.mjs config
test-local-admin:
	cd server/auth && go test -race ./internal/localadmin ./cmd/local-admin
	node --test scripts/tests/local-stack.test.mjs
	node --test scripts/tests/local-workflow-key.test.mjs
	node --test scripts/tests/local-k8s-open.test.mjs
test-local-admin-live:
	node scripts/local-admin-smoke.mjs

.PHONY: test-admin-setup-live
test-admin-setup-live:
	$(NODE) scripts/admin-setup-smoke.mjs

.PHONY: fresh-up fresh-down
fresh-up: local-services-up

fresh-down: local-services-down

# Pass file paths, never credential values. Export also supports make VAR=value.
export KEYCLOAK_URL KEYCLOAK_REALM RUNLOOM_ADMIN_TOKEN_FILE RUNLOOM_PROVISIONER_SECRET_PATH RUNLOOM_PROVISIONER_SECRET_GID
export RUNLOOM_TEST_TEMPORAL_ADDRESS
export RUNLOOM_SECRETS_KEY_PATH RUNLOOM_SECRETS_KEY_GID
export RUNLOOM_DATABASE_SECRET_PATH RUNLOOM_LLM_API_KEY_PATH
export RUNLOOM_BASE_CONFIG_PATH RUNLOOM_API_CONFIG_PATH RUNLOOM_WORKER_CONFIG_PATH
export RUNLOOM_CONSUMER_CONFIG_PATH RUNLOOM_VAULT_CONFIG_PATH RUNLOOM_EMAIL_CONFIG_PATH RUNLOOM_PRICING_CONFIG_PATH

.DEFAULT_GOAL := help
.PHONY: test-edit-locks
test-edit-locks:
	cd server/workflows && go test -race ./internal/store ./internal/api -run 'EditLock|EditLease|WorkflowEditLock'
	node scripts/edit-locks-smoke.mjs
.PHONY: test-auth-theme
.PHONY: test-keycloak-admin-live
test-keycloak-admin-live:
	$(NODE) scripts/keycloak-admin-smoke.mjs

test-auth-theme:
	cd studio/libraries/platform && npm test
	node scripts/auth-theme-smoke.mjs
.PHONY: setup-server setup-studio build-server build-studio test-server test-studio \
	run-api run-worker run-studio docker-build-server docker-build-studio

# Component-named aliases; older Go/UI targets remain compatible.
setup-server: setup-go
setup-studio: setup-ui
build-server: build-go
build-studio: build-ui
test-server: test-go
test-studio: test-ui
run-api: run-go-api
run-worker: run-go-worker
run-studio: run-ui
docker-build-server: docker-build-go
docker-build-studio: docker-build-ui
.PHONY: setup-workflow-key docker-config-secrets docker-up-secrets test-ui-api-nodes test-ui-secrets test-workflow-key-config
.PHONY: setup-api-docs build-api-docs test-api-docs

setup-api-docs:
	cd server/api-docs && npm ci --ignore-scripts

build-api-docs:
	cd server/api-docs && npm run build

test-api-docs:
	cd server/api-docs && npm test
	cd server/workflows && go test ./internal/api -run 'TestAPIDocs|TestStudioOpenAPI|TestConsumerOpenAPI|TestAPIVersionRouting|TestSwaggerBrowser' -count=1

.PHONY: help setup setup-go setup-ui test test-go test-ui check-go check-branding \
	run-temporal run-go-api run-go-worker run-ui \
	setup-website run-website build-website preview-website test-website \
	docker-build docker-build-go docker-build-ui \
	docker-up-go docker-up-go-mongo docker-down-go docker-logs-go \
	docker-up docker-up-mongo docker-down docker-logs docker-config smoke helm-lint helm-template helm-package
.PHONY: build build-go build-ui check verify vet-go test-go-race test-consumer \
	test-ui-browser test-keycloak-live test-temporal-live setup-consumer-auth \
	check-consumer-secret docker-config-consumer docker-up-consumer docker-up-consumer-mongo

help:
	@echo "Independent documentation website (no containers)"
	@echo "  make setup-docs run-docs    Install dependencies, then serve docs on port 4400"
	@echo "  make build-docs            Build prerendered static documentation"
	@echo "  make preview-docs          Preview the production documentation on port 4400"
	@echo "  make test-docs             Test content and boundaries, then build"
	@echo "  make test-docs-browser     Build and run desktop/mobile browser checks"
	@echo "  make sync-docs             Explicitly refresh packaged guides and API contracts"
	@echo "  make check-docs-sync       Check packaged content against repository sources"
	@echo "  make docker-build-docs     Build the independent documentation image"
	@echo "  make docker-up-docs        Run documentation alone on localhost:4400"
	@echo "  make k8s-docs              Forward Kind documentation to localhost:4400"
	@echo ""
	@echo "Source archive"
	@echo "  make zip                   ZIP sources to dist/runloom-source-NNN.zip (next number)"
	@echo ""
	@echo "Formatting"
	@echo "  make format                Run each app's formatter and format changed Go files"
	@echo "  make format-check          Read-only per-app and changed Go formatting checks"
	@echo ""
	@echo "Local development"
	@echo "  make local-up              Start local stack and ensure the default admin"
	@echo "  make local-admin           Ensure local admin and team member; preserve passwords"
	@echo "  make local-admin-reset     Restore both public local passwords; end their sessions"
	@echo "  make local-down            Stop the local stack; retain all volumes"
	@echo "  make local-config          Validate local-only startup safety checks"
	@echo "  make test-local-admin      Test local bootstrap and startup guards"
	@echo "  make test-admin-setup-live Verify login and repeat setup; select SERVICES_DATABASE"
	@echo "  make docker-build-auth-api Build only the Auth runtime image"
	@echo "  make docker-build-local-admin Build only the local account setup image"
	@echo "  Components: server/ (Go API and worker), studio/ (Angular builder)"
	@echo "  make run-api run-worker run-studio  Run individually in separate terminals"
	@echo "  make build-server build-studio     Build components individually"
	@echo "  make setup                 Refresh Go vendors, build services and install Angular dependencies"
	@echo "  make vendor-go             Refresh standalone Go vendors using the local SDK sources"
	@echo "  make build                 Build Go binaries in bin/ and Angular studio"
	@echo "  make test                  Run Go tests and build the Angular studio"
	@echo "  make check                 Check branding, Go vet and workflow determinism"
	@echo "  make verify                Check, race-test, build UI, validate Compose/Helm"
	@echo "  make test-go-race          Run all Go tests with the race detector"
	@echo "  make test-code             Test the JavaScript node and isolated runner"
	@echo "  make test-email            Test SMTP delivery, validation and workflow mapping"
	@echo "  make test-llm-costs        Test LLM accounting, persistence and run API"
	@echo "  make test-llm-costs-live   Local Temporal retry/child/replay test; fake LLM only"
	@echo "  make test-ui-costs-email   Browser checks for Email and run costs; needs dev UI"
	@echo "  make run-temporal          Start the local Temporal development server"
	@echo "  make run-go-api            Start the Go API"
	@echo "  make run-go-worker         Start the Go worker"
	@echo "  make run-ui                Start the Angular studio on port 4200"
	@echo ""
	@echo "Consumer authentication and opt-in integration checks"
	@echo "  make setup-consumer-auth   Bootstrap Keycloak; requires private file paths"
	@echo "  make k8s-consumer-setup    Verify/repair local provisioning (already included in k8s-up/install/upgrade)"
	@echo "  make setup-workflow-key    Create/reuse the automatic local key, or create an explicit key file"
	@echo "  make docker-up-secrets     Start full stack with encrypted workflow secrets"
	@echo "  make docker-config-secrets Validate workflow secret mounts"
	@echo "  make test-workflow-key-config Check Helm/ESO and Compose key configuration"
	@echo "  make test-ui-api-nodes     Browser checks for API, End and workflow secrets"
	@echo "  make test-ui-secrets       Browser checks against the isolated Go vault fixture"
	@echo "  make test-auth-theme       Redirect and live branded login checks; needs Studio"
	@echo "  make test-keycloak-admin-live Verify local Keycloak admin console login and realm navigation"
	@echo "  make test-edit-locks       Edit-lock tests and live two-member browser checks"
	@echo "  make test-consumer         Run consumer and API package tests"
	@echo "  make setup-api-docs        Install pinned Swagger maintenance tools"
	@echo "  make build-api-docs        Generate Studio OpenAPI and local Swagger assets"
	@echo "  make test-api-docs         Validate specs, routes and auth boundaries"
	@echo "  make test-ui-browser       Check navigation, editing and settings; needs dev UI"
	@echo "  make test-keycloak-live    Create/test/remove a disposable Keycloak container"
	@echo "  make test-temporal-live    Test against RUNLOOM_TEST_TEMPORAL_ADDRESS"
	@echo "  See docs/consumer-api.md for credentials and integration prerequisites"
	@echo ""
	@echo "Independent product website (no containers)"
	@echo "  make setup-website         Install website dependencies only"
	@echo "  make run-website           Start website development on port 4300"
	@echo "  make build-website         Build static production website files"
	@echo "  make preview-website       Preview the built website on port 4301"
	@echo "  make test-website          Run website content tests and build"
	@echo ""
	@echo "Docker"
	@echo "  make docker-build          Build Go and Angular images"
	@echo "  make docker-build-ui       Build the Angular studio image"
	@echo "  make check-docker-security Check Dockerfile/context hardening rules"
	@echo "  make test-docker-images    Smoke-test built local images in restricted containers"
	@echo "  make scan-docker-images    Test and scan built local images; fail on high/critical findings"
	@echo "  make docker-up-go          Build and run the complete Go stack"
	@echo "  make docker-up-go-mongo    Run Go with MongoDB instead of PostgreSQL"
	@echo "  make docker-logs-go        Follow the Go stack logs"
	@echo "  make docker-down-go        Stop the Go stack"
	@echo "  make docker-up             Build and start the full stack; wait for health"
	@echo "  make docker-up-mongo       Start the stack using MongoDB"
	@echo "  make docker-down           Stop the stack and retain its volumes"
	@echo "  make docker-config         Validate Compose configuration"
	@echo "  make docker-config-consumer Validate consumer overlay; needs secret file path"
	@echo "  make docker-up-consumer    Start full stack with service-account provisioning"
	@echo "  make docker-up-consumer-mongo Start consumer-enabled stack using MongoDB"
	@echo "  make smoke                 Exercise a synthetic workflow through the UI"
	@echo "  make helm-lint             Check development and external-service charts"
	@echo "  make helm-template         Render Kubernetes manifests locally"
	@echo "  make helm-package          Package the chart in dist/charts"
	@echo ""
	@echo "Local Kubernetes (kind; never uses the selected remote context)"
	@echo "  make k8s-cluster           Create or reuse the local kind cluster"
	@echo "  make k8s-up                Create cluster, provision databases/admin, build and install"
	@echo "  make k8s-down              DELETE the local kind cluster and ALL its persistent data"
	@echo "  make k8s-install           Build/load images and install a new Helm release"
	@echo "  make k8s-upgrade           Rebuild/load images and upgrade an existing release"
	@echo "  make k8s-run               Forward Studio to localhost (keep terminal open)"
	@echo "  make k8s-open              Start/open product website linked to Kubernetes Studio"
	@echo "  make k8s-status            Show local release, pods, services and volumes"
	@echo "  make k8s-logs              Follow all namespace pod/container logs (Ctrl+C stops tailing)"
	@echo "  make k8s-test              Run the installed chart's connectivity test"
	@echo "  make test-k8s-services     Verify local browser/API flows and org namespaces"
	@echo "  make test-local-k8s        Test command orchestration without changing a cluster"
	@echo "  See docs/deployment.md for prerequisites and K8S_* settings"

setup: setup-go setup-ui

.PHONY: vendor-go
vendor-go:
	@GO="$(GO)" sh scripts/vendor-go.sh

setup-go: vendor-go
	@for dir in server/auth server/workflows server/management server/gateway server/assistant tools/database-setup; do $(MAKE) -C $$dir build || exit $$?; done

setup-ui:
	cd studio/projects/host && npm ci
	cd studio/projects/workflows && npm ci
	cd studio/projects/management && npm ci
	cd studio/projects/platform-admin && npm ci
	cd studio/libraries/platform && npm ci

build: build-go build-ui

build-go:
	@for dir in server/auth server/workflows server/management server/gateway server/assistant tools/database-setup; do $(MAKE) -C $$dir build || exit $$?; done

build-ui: build-host build-workflows build-management build-platform-admin

test: check-branding test-go test-ui

check: check-branding vet-go check-go

# No deployment, bootstrap, browser or live integration tests are implicit here.
verify: check test-go-race build-ui docker-config helm-lint

check-branding:
	node scripts/check-branding.mjs

test-go: check-assistant-contract
	@for dir in server/auth server/workflows server/management server/gateway server/assistant tools/database-setup; do $(MAKE) -C $$dir test || exit $$?; done

.PHONY: test-email test-llm-costs test-ui-costs-email test-llm-costs-live
test-email:
	cd server/workflows && go test -race ./internal/mailnode ./internal/definition ./internal/engine -run 'Email|SMTP|SendEnvelope'

test-llm-costs:
	cd server/workflows && go test -race ./internal/budget ./internal/usage ./internal/store ./internal/activities ./internal/api ./internal/engine -run 'Policy|Budget|Usage|Analytics|Metered|LLM|GatewayTenant|ConsumerAuthorization'

test-ui-costs-email:
	node scripts/costs-email-smoke.mjs

test-llm-costs-live:
	@test -n "$$RUNLOOM_TEST_TEMPORAL_ADDRESS" || { echo "Set RUNLOOM_TEST_TEMPORAL_ADDRESS to local/disposable Temporal." >&2; exit 1; }
	cd server/workflows && go test ./internal/api -run '^TestLLM(Usage|Budget)Live$$' -count=1 -timeout 120s

test-go-race:
	@for dir in server/auth server/workflows server/management server/gateway server/assistant tools/database-setup; do $(MAKE) -C $$dir race || exit $$?; done

vet-go:
	@for dir in server/auth server/workflows server/management server/gateway server/assistant tools/database-setup; do $(MAKE) -C $$dir vet || exit $$?; done

test-consumer:
	cd server/workflows && go test ./internal/consumer ./internal/api

check-go:
	cd server/workflows && "$$(go env GOPATH)/bin/workflowcheck" ./...

test-ui: build-ui
	cd studio/libraries/platform && npm test
	cd studio/projects/host && npm test
	cd studio/projects/workflows && npm test
	cd studio/projects/management && npm test
	cd studio/projects/platform-admin && npm test

test-ui-browser:
	node studio/projects/workflows/tests/dashboard.browser.mjs
	node scripts/graph-builder-smoke.mjs
	node scripts/icon-alignment-smoke.mjs
	node scripts/unsaved-changes-smoke.mjs
	node scripts/service-accounts-smoke.mjs
	node scripts/management-dialogs-smoke.mjs
	node scripts/code-node-smoke.mjs
	node scripts/costs-email-smoke.mjs
	node scripts/notifications-smoke.mjs
	node scripts/workspace-navigation-smoke.mjs
	node scripts/cost-analytics-smoke.mjs
	node scripts/budget-policies-smoke.mjs
	node scripts/theme-smoke.mjs
	node scripts/federation-smoke.mjs
	node studio/projects/host/tests/navigation.browser.mjs

test-keycloak-live:
	cd server/workflows && RUNLOOM_TEST_KEYCLOAK=1 go test ./internal/consumer -run '^TestKeycloakProvisioningLive$$' -count=1 -timeout 4m

test-temporal-live:
	@test -n "$$RUNLOOM_TEST_TEMPORAL_ADDRESS" || { echo "Set RUNLOOM_TEST_TEMPORAL_ADDRESS to a disposable Temporal server." >&2; exit 1; }
	cd server/workflows && go test ./internal/api -run '^(TestConsumerExecutionLive|TestSchedulesAndChildrenLive)$$' -count=1 -timeout 4m

setup-consumer-auth:
	@test -n "$$RUNLOOM_ADMIN_TOKEN_FILE" && test -f "$$RUNLOOM_ADMIN_TOKEN_FILE" && test -r "$$RUNLOOM_ADMIN_TOKEN_FILE" || { echo "Set RUNLOOM_ADMIN_TOKEN_FILE to a readable private admin-token file." >&2; exit 1; }
	@test -n "$$RUNLOOM_PROVISIONER_SECRET_PATH" || { echo "Set RUNLOOM_PROVISIONER_SECRET_PATH to a new private output file." >&2; exit 1; }
	@test ! -e "$$RUNLOOM_PROVISIONER_SECRET_PATH" && test ! -L "$$RUNLOOM_PROVISIONER_SECRET_PATH" || { echo "Provisioner output already exists; refusing to overwrite it." >&2; exit 1; }
	@node scripts/setup-consumer-auth.mjs "$$KEYCLOAK_URL" "$$KEYCLOAK_REALM" "$$RUNLOOM_ADMIN_TOKEN_FILE" "$$RUNLOOM_PROVISIONER_SECRET_PATH"

setup-workflow-key:
	@node scripts/create-workflow-key.mjs

docker-config-secrets:
	$(COMPOSE) -f compose/compose.secrets.yaml config --quiet

docker-up-secrets: local-services-up

test-workflow-key-config:
	$(NODE) --test scripts/tests/workflow-key-chart.test.mjs scripts/tests/workflow-key-compose.test.mjs scripts/tests/local-workflow-key.test.mjs scripts/tests/local-stack.test.mjs

test-ui-api-nodes:
	node scripts/api-nodes-smoke.mjs

test-ui-secrets:
	node scripts/workflow-secrets-smoke.mjs

run-temporal:
	mkdir -p var/runloom
	temporal server start-dev --db-filename var/runloom/temporal.db

run-go-api: build-code
	cd server/workflows && RUNLOOM_CODE_RUNNER="$(CURDIR)/server/workflows/bin/runloom-code" go run ./cmd/workflows

run-go-worker: build-code
	cd server/workflows && RUNLOOM_CODE_RUNNER="$(CURDIR)/server/workflows/bin/runloom-code" go run ./cmd/worker

.PHONY: build-code
build-code:
	mkdir -p server/workflows/bin
	cd server/workflows && go build -o bin/runloom-code ./cmd/code

.PHONY: test-code
test-code:
	cd server/workflows && go test ./internal/coderunner ./internal/codeexec ./internal/activities ./internal/definition ./internal/engine ./internal/api

run-ui:
	node scripts/studio-dev-server.mjs

setup-website:
	cd Website && npm ci

run-website:
	cd Website && npm start

build-website:
	cd Website && npm run build

preview-website:
	cd Website && npm run preview

test-website:
	cd Website && npm test && npm run build

.PHONY: setup-docs run-docs build-docs preview-docs test-docs test-docs-browser sync-docs check-docs-sync
setup-docs:
	cd docs-site && npm ci
run-docs:
	cd docs-site && npm start
build-docs:
	cd docs-site && npm run build
preview-docs:
	cd docs-site && npm run preview
test-docs:
	cd docs-site && npm test && npm run build
test-docs-browser:
	cd docs-site && npm run test:e2e
sync-docs:
	cd docs-site && npm run sync
check-docs-sync:
	cd docs-site && npm run sync:check

docker-build: check-docker-security docker-build-go docker-build-ui docker-build-docs

.PHONY: docker-build-docs docker-up-docs k8s-docs
DOCS_IMAGE ?= runloom-docs:local
DOCS_PORT ?= 4400
docker-build-docs:
	docker build -t "$(DOCS_IMAGE)" docs-site
docker-up-docs:
	DOCS_IMAGE="$(DOCS_IMAGE)" DOCS_PORT="$(DOCS_PORT)" $(DOCKER) compose --project-directory . -p runloom-docs -f compose/compose.yaml up -d --build --wait --no-deps docs
k8s-docs:
	node scripts/local-k8s.mjs docs

.PHONY: check-docker-security test-docker-images scan-docker-images
check-docker-security:
	$(NODE) --test scripts/tests/docker-hardening.test.mjs
test-docker-images:
	$(NODE) scripts/verify-docker-images.mjs
scan-docker-images:
	$(NODE) scripts/verify-docker-images.mjs --scan

.PHONY: docker-build-local-admin docker-build-auth-api
.PHONY: docker-build-worker docker-build-workflows-api
docker-build-worker:
	$(MAKE) -C server/workflows docker-build-worker WORKER_IMAGE="$(WORKER_IMAGE)"
docker-build-workflows-api:
	$(MAKE) -C server/workflows docker-build-api

docker-build-local-admin:
	$(MAKE) -C server/auth docker-build-local-admin LOCAL_ADMIN_IMAGE="$(LOCAL_ADMIN_IMAGE)"
docker-build-auth-api:
	$(MAKE) -C server/auth docker-build-api

docker-build-go:
	@for dir in server/auth server/workflows server/management server/gateway server/assistant tools/database-setup; do $(MAKE) -C $$dir docker-build || exit $$?; done

.PHONY: docker-build-host docker-build-workflows docker-build-management build-host build-workflows build-management build-platform
docker-build-ui: docker-build-host docker-build-workflows docker-build-management docker-build-platform-admin

.PHONY: docker-build-platform-admin build-platform-admin test-platform-admin test-platform-live
docker-build-platform-admin:
	$(DOCKER) build --tag $(PLATFORM_UI_IMAGE) studio/projects/platform-admin
build-platform-admin:
	cd studio/projects/platform-admin && npm run build
test-platform-admin:
	cd studio/projects/platform-admin && npm test
test-platform-live:
	$(NODE) scripts/platform-database-test.mjs
	$(NODE) scripts/platform-smoke.mjs

docker-build-host:
	$(DOCKER) build --tag $(UI_IMAGE) studio/projects/host

docker-build-workflows:
	$(DOCKER) build --tag $(WORKFLOWS_UI_IMAGE) studio/projects/workflows

docker-build-management:
	$(DOCKER) build --tag $(MANAGEMENT_UI_IMAGE) studio/projects/management

build-host:
	cd studio/projects/host && npm run build

build-workflows:
	cd studio/projects/workflows && npm run build

build-management:
	cd studio/projects/management && npm run build

build-platform:
	cd studio/libraries/platform && npm run build

docker-up-go: docker-up

docker-up-go-mongo: docker-up-mongo

docker-down-go: docker-down

docker-logs-go: docker-logs

docker-up:
	$(NODE) scripts/local-services.mjs up

docker-up-mongo:
	SERVICES_DATABASE=mongodb $(NODE) scripts/local-services.mjs up

docker-down:
	$(NODE) scripts/local-services.mjs down

docker-logs:
	$(NODE) scripts/local-services.mjs logs

docker-config:
	$(COMPOSE) config --quiet
	$(DOCKER) compose --project-directory "$(CURDIR)" -f compose/compose.server.yaml config --quiet

check-consumer-secret:
	@test -n "$$RUNLOOM_PROVISIONER_SECRET_PATH" && test -f "$$RUNLOOM_PROVISIONER_SECRET_PATH" && test -r "$$RUNLOOM_PROVISIONER_SECRET_PATH" || { echo "Set RUNLOOM_PROVISIONER_SECRET_PATH to a readable provisioner secret file." >&2; exit 1; }

docker-config-consumer: check-consumer-secret
	$(COMPOSE) -f compose/compose.consumer.yaml config --quiet

docker-up-consumer: local-services-up

docker-up-consumer-mongo: docker-up-mongo

smoke:
	node scripts/smoke.mjs

helm-lint:
	sh scripts/check-helm.sh

helm-template:
	$(HELM) template runloom charts/runloom

helm-package:
	mkdir -p dist/charts
	$(HELM) package charts/runloom --destination dist/charts

# Controller is a separate, explicitly enabled release. These targets never deploy.
.PHONY: helm-ingress-deps helm-ingress-template helm-ingress-package
helm-ingress-deps:
	$(HELM) dependency build charts/runloom-ingress --skip-refresh
helm-ingress-template:
	$(HELM) template runloom-ingress charts/runloom-ingress --set nginx.enabled=true
helm-ingress-package:
	mkdir -p dist/charts
	$(HELM) package charts/runloom-ingress --destination dist/charts

.PHONY: k8s-cluster k8s-up k8s-down k8s-install k8s-upgrade k8s-run k8s-open k8s-status k8s-logs k8s-test test-local-k8s k8s-consumer-setup

k8s-consumer-setup:
	$(NODE) scripts/local-k8s-consumer.mjs
k8s-cluster:
	$(NODE) scripts/local-k8s.mjs cluster

k8s-up:
	$(NODE) scripts/local-k8s.mjs up

k8s-down:
	$(NODE) scripts/local-k8s.mjs down

k8s-install:
	$(NODE) scripts/local-k8s.mjs install

k8s-upgrade:
	$(NODE) scripts/local-k8s.mjs upgrade

k8s-run:
	$(NODE) scripts/local-k8s.mjs run

k8s-open:
	$(NODE) scripts/local-k8s.mjs open

k8s-status:
	$(NODE) scripts/local-k8s.mjs status

k8s-logs:
	$(NODE) scripts/local-k8s.mjs logs

k8s-test:
	$(NODE) scripts/local-k8s.mjs test

.PHONY: test-k8s-services
test-k8s-services:
	$(NODE) scripts/test-k8s-services.mjs

test-local-k8s:
	$(NODE) --test scripts/tests/local-consumer-auth.test.mjs scripts/tests/setup-consumer-auth.test.mjs scripts/tests/local-k8s-forward.test.mjs
	$(NODE) --test scripts/tests/local-k8s.test.mjs scripts/tests/local-k8s-logs.test.mjs
	$(NODE) --test scripts/tests/local-k8s-open.test.mjs
	$(NODE) --test scripts/tests/local-k8s-consumer.test.mjs
	$(NODE) --test scripts/tests/local-k8s-services.test.mjs

.PHONY: build-auth-service build-workflows-service build-management-service run-auth run-management run-gateway check-service-boundaries
build-auth-service:
	$(MAKE) -C server/auth build
build-workflows-service:
	$(MAKE) -C server/workflows build
build-management-service:
	$(MAKE) -C server/management build
run-auth:
	$(MAKE) -C server/auth run
run-management:
	$(MAKE) -C server/management run
run-gateway:
	$(MAKE) -C server/gateway run
check-service-boundaries:
	$(NODE) scripts/check-service-boundaries.mjs
.PHONY: test-database-connections
test-database-connections:
	$(NODE) scripts/database-connections-smoke.mjs

.PHONY: build-assistant test-assistant run-assistant
build-assistant:
	$(MAKE) -C server/assistant build
test-assistant:
	$(MAKE) -C server/assistant test
.PHONY: eval-lumi eval-lumi-live
eval-lumi:
	cd server/assistant && GOWORK=off RUNLOOM_LUMI_EVAL_LIVE=0 $(GO) test -mod=vendor -count=1 -v ./internal/chat -run '^TestLumiWorkflowEval$$'
eval-lumi-live:
	cd server/assistant && GOWORK=off RUNLOOM_LUMI_EVAL_LIVE=1 $(GO) test -mod=vendor -count=1 -timeout=30m -v ./internal/chat -run '^TestLumiWorkflowEval$$'

.PHONY: test-assistant-live
test-assistant-live:
	$(MAKE) -C tools/database-setup docker-build
	$(NODE) scripts/test-assistant.mjs
run-assistant:
	$(MAKE) -C server/assistant run

.PHONY: test-assistant-browser
test-assistant-browser: build-assistant
	$(NODE) scripts/test-assistant-browser.mjs

.PHONY: test-authoring
test-authoring:
	$(NODE) scripts/test-authoring.mjs

.PHONY: test-building test-building-live test-building-browser
test-building:
	cd server/workflows && go test -race ./tests -run 'Building|ForEach|Integration|LLMTask' -count=1
	cd studio/projects/workflows && npm test

test-building-live:
	@test -n "$$RUNLOOM_TEST_TEMPORAL_ADDRESS" || { echo "Set RUNLOOM_TEST_TEMPORAL_ADDRESS to a disposable local Temporal server." >&2; exit 1; }
	cd server/workflows && go test ./tests ./internal/api -run 'TestBuildingLiveReplay|TestDecisionLive' -count=1 -timeout 120s

test-building-browser:
	$(NODE) studio/projects/workflows/tests/building-nodes.browser.mjs

.PHONY: sync-assistant-contract check-assistant-contract
sync-assistant-contract:
	cp spec/workflow.schema.json server/assistant/internal/chat/workflow.schema.json
check-assistant-contract:
	cmp spec/workflow.schema.json server/assistant/internal/chat/workflow.schema.json
