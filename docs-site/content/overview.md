# What is Runloom?

Runloom helps teams design workflows visually, save them as JSON definitions and run them durably with Temporal. Studio is the place to build and test; management views help you organize access, integrations and operations.

## Understand the workspace

An **organization** contains teams. A **team** controls access to its workflows and integrations. A **project** groups workflows for authoring and testing; it is not a separate runtime permission boundary.

A **workflow definition** describes the steps. Its **draft** is editable. Publishing creates an immutable numbered **version**. A **run** is one execution, with its own input, progress and result.

## Start building

1. [Install locally with Docker Compose](/getting-started/docker-compose), or use [a Kind cluster](/getting-started/kubernetes).
2. [Create your first workflow](/guides/first-workflow) and test its draft.
3. Review the result, then publish when it is ready for production starts.

Testing is real execution, not a simulation. External API, email, database and LLM nodes can change external systems or incur charges.

## Connect and coordinate

Use [database connections](/integrations/connections), [External API nodes and secrets](/integrations/secrets), and [email](/nodes/email) to integrate your tools. The [node reference](/nodes/reference) explains conditions, parallel branches, waits, callbacks, approvals and explicit pauses.

[Subworkflows](/guides/schedules-and-subworkflows) let parents call reusable published children. [Lumi](/guides/lumi) offers scoped chat and reviewed authoring proposals; it is not unrestricted autonomous access to your account.

## Operate with clear boundaries

[Consumer APIs](/api/consumer) use service-account permissions. [Studio APIs](/api/studio) use human identity and team access. [Platform administrators and staff](/management/platform) have separate platform permissions, not automatic access to tenant workflows.

Read [cost accounting](/management/costs) for the difference between estimated LLM spend and provider invoices. Security configuration and operational controls remain the operator's responsibility; using Runloom is not a compliance certification.
