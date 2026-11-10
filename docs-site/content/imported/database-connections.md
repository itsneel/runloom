# Database connections and nodes

## 1. Create a connection

Open **Management → Connections** (`/management/connections`) and select **Add connection**. A team admin chooses MongoDB, PostgreSQL, MySQL or Supabase, enters the host, port, database and username, then saves the password. Use a dedicated, least-privilege database account. Supabase uses a PostgreSQL direct or session-pooler connection and its database password, not a Supabase REST API key.

Passwords are encrypted using the existing workflow-secret key and are never returned by the API. Editing without a password preserves the existing one; entering a replacement rotates it. Connection IDs are team-local and immutable in the editor. A workflow stores only the connection ID. Changing settings affects future invocations, including published workflows; disabling or deleting prevents future use but does not roll back completed or in-flight queries.

Saving an enabled connection automatically tests the saved settings. A successful save with a failed test stays saved; the editor remains open with an actionable error so you can correct it and save again. Disabled connections are saved without testing. A passing test confirms connectivity/authentication, not every query permission; use Schema and a database-node test to check those. For MongoDB, Authentication database must be the database where the user was created; blank means `admin`, not the data database. MongoDB does not distinguish a wrong authentication database from a wrong username/password, so authentication errors explain all three possibilities. Driver errors are mapped to credential-free authentication, permission, missing database, DNS, refused connection, timeout or TLS messages; raw driver text is never returned.

Team members can list connection metadata. Editors and admins can test connections, discover schemas and use connections in workflows. Only team admins can create, edit or delete connections. Organization owners/admins inherit team-admin access. Consumer credentials cannot manage or discover connections. Save/delete operations create audit events without credentials. Test/schema calls do not create database writes, but schema discovery reads metadata and, for MongoDB, samples documents.

## 2. Choose permissions

| Access | Operations |
| --- | --- |
| Read only (default) | Guided reads, MongoDB read aggregation, SQL starting with `SELECT` in a read-only transaction |
| Read and write | Guided insert/update/delete plus the read operations |
| SQL and schema changes | The above plus arbitrary single SQL statements, including `CREATE TABLE`, `ALTER TABLE` and raw SQL writes |

Permissions narrow the connector surface, not the database user's underlying privileges. Always enforce appropriate grants, row-level security and resource limits in the database. Do not connect as a database administrator. MySQL DDL can commit implicitly. Read-only transactions are not a sandbox for privileged functions or all external side effects. For MongoDB, create collections by inserting documents; arbitrary database/admin commands and server-side JavaScript are not supported.

## 3. Build a Database node

1. Add **Database** from the workflow node library and choose a saved connection.
2. Select an operation. For guided operations, click **Discover schema**, select a table/collection, and map the discovered fields. A literal, JSON value or `${input.customer}` / `${steps.previous.output.field}` reference can be used. Blank mapped fields are omitted; enter `null` for an explicit null. Manual field mapping is available in JSON.
3. Supply equality filters for reads, updates and deletes. Updates/deletes require a nonempty filter and affect **all** matching records. `Maximum returned records` does not limit write effects. Put destructive operations behind an Approval node when appropriate.
4. For SQL, use `$1`, `$2` for PostgreSQL/Supabase or `?` for MySQL, with the matching values in `parameters`. Workflow expressions are allowed in parameter values, never in SQL text, table/schema identifiers or connection IDs. One statement per node; omit semicolons. Read-only/raw read queries must start with `SELECT`; CTE queries require schema access in this first version.
5. For MongoDB aggregation, enter `pipeline` JSON. Supported stages are `$match`, `$project`, `$group`, `$sort`, `$limit`, `$skip`, `$unwind`, `$count`, `$addFields`, `$set`, `$unset`, `$replaceRoot` and `$replaceWith`. Write stages and server-side JavaScript are rejected. Basic `filter` is equality-only; use `$match` in a pipeline for richer read predicates.
6. Save the draft and use **Test workflow**. Tests execute real queries and writes; publishing is not required. Invalid drafts remain saveable, but invalid definitions cannot publish. Malformed editor JSON must be corrected or discarded before saving because it cannot represent a workflow definition.

SQL discovery reads `information_schema`, bounded to 2,000 columns and supported identifiers. MongoDB discovers up to 100 collections and samples five documents per collection. Its displayed fields are inferred, not enforced validators or a complete collection schema. Empty collections may show no fields. Guided identifiers support ASCII letters, digits and underscores, starting with a letter/underscore, up to 63 characters. Use SQL mode for other SQL identifiers.

## 4. Output and failure rules

The node returns `rows`, `affected` and `truncated`. SQL 64-bit integers and byte/decimal values are returned as strings to avoid JavaScript integer precision loss. Guided writes return an affected count; SQL mode reports rows (for example `RETURNING`) but does not promise an affected count for non-row-returning statements. MongoDB results are JSON-serialized BSON values, not a lossless Extended JSON round trip. Types such as ObjectId cannot be reconstructed from plain string equality filters in this first version.

The default returned-row limit is 100; maximum 1,000. Output is limited to 256 KiB, configuration to 64 KiB, SQL text to 32 KiB. At most eight connector calls run concurrently per API/worker process. A call has a 20-second database deadline (connection tests: eight seconds), within a 25-second activity timeout and 60-second scheduling deadline. The activity has one attempt; MongoDB driver retries are disabled. These bounds do not replace database-side memory/query limits.

Timeout/cancellation or a missing acknowledgement does **not** prove that a write was rolled back. Inspect the external database before retrying or restarting. Use unique business keys or idempotent SQL for writes. Runloom does not provide exactly-once external database effects. Individual nodes use separate connections/transactions; there is no transaction spanning nodes.

Passwords are resolved only inside activities, not passed through Temporal inputs. Returned values are redacted using the existing sensitive-key rules and exact credential matching before activity completion. Arbitrary query results can still contain sensitive business data; select only required fields and apply the platform's access, retention and output-redaction controls. Do not embed passwords in query text, parameters, workflow inputs or examples.

## 5. Operator configuration

Both the Workflows API and worker need the existing workflow-secret encryption key and outbound network access. Public database endpoints require verified TLS using the container/system trust store. Self-signed certificates/custom CA upload are not supported. Private, loopback or plaintext destinations require exact `host:port` operator approval; redirects through MongoDB replica-set members must also satisfy that policy. Link-local/metadata destinations and the configured Runloom storage endpoint (including DNS aliases) are blocked. Never approve other Runloom infrastructure databases; use network egress policy as an additional boundary.

Helm configuration (mounted into Workflows and worker only):

```yaml
databaseConnections:
  allowedAddresses:
    - reporting-db.example.internal:5432
```

Compose setup reads `compose/config/database-connections.yaml` and includes it in generated Workflows/worker configuration. Set `RUNLOOM_DATABASE_NODE_ALLOWED_ADDRESSES` there to a comma-separated list of exact destinations, then run your existing Compose setup command. Native deployments may supply the same key in their mounted application YAML. Configuration changes require restart. Inside containers, `localhost` refers to that container, not the host; use an explicitly approved reachable hostname such as `host.docker.internal` for a host-side fixture.

The API is documented in the **Connections** operations of the Studio Swagger/OpenAPI document. No connection-management routes are added to the consumer execution API.

## 6. Verification

`make test-database-connections` starts isolated, resource-limited PostgreSQL, MongoDB replica-set and MySQL fixtures, runs live CRUD, discovery, permission, parameter-binding and encrypted-persistence checks, then removes those exact containers and their temporary data. It does not start or modify the Runloom application stack. Images must be available or pullable. The Supabase provider path is exercised against PostgreSQL; hosted Supabase TLS/network/pooler behavior requires a real Supabase test project.

See the [JSON contract](json-contract.md), [workflow schema](../spec/workflow.schema.json), [example](../examples/database-query.json), and [external API/secret guide](api-nodes-and-secrets.md).
