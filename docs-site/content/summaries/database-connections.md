# Database connections

Connect MongoDB, PostgreSQL, MySQL or Supabase, then use the saved connection in a Database node.

## Connect and query

1. A team admin opens **Management → Connections → Add connection**.
2. Enter database details and a least-privilege account. Saving an enabled connection automatically tests it.
3. Add a **Database** node. Choose the connection and operation.
4. Discover schema and map fields, or enter a parameterized SQL query / supported MongoDB pipeline.
5. Test with safe data. Tests execute real queries and writes.

## Avoid common mistakes

For MongoDB, use the authentication database where the user was created; blank defaults to `admin`. Supabase uses database credentials, not a REST API key. Private hosts require operator approval.

A failed connection test does not undo the saved connection. Fix its settings and save again.

Updates and deletes affect **all matching records**; the returned-row limit does not limit writes. A timeout does not prove rollback—inspect the database before retrying. Keep credentials out of queries and workflow inputs.

Expand the reference for connector permissions, parameters and output limits.
