# Runloom gateway

This is an independent Go module. Run `make build`, `make test`, or `make docker-build` in this directory. Its Docker context is this directory alone. No parent module, Go workspace, sibling source, or sibling build is required.

Dependencies are pinned in go.mod and committed in vendor. The authentication SDK, where used, is an independent versioned dependency, not a shared source directory. Runtime configuration is mounted using RUNLOOM_CONFIG_FILE; credentials are read from referenced files, never baked into images.
