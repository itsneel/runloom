# Runloom Workflows

Independent Go service for workflow definitions, execution, schedules, secrets, and the Temporal worker. Build from this directory with `make build` or `docker build -t runloom-workflows .`.

Auth and Management are HTTP dependencies only. Their source trees are not build inputs. The authentication SDK is pinned and vendored; it is developed and released independently from `sdk/go-auth`.
