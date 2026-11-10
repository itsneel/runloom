package dbnode_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/dbnode"
	"github.com/stretchr/testify/require"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Only run against an explicitly supplied DISPOSABLE fixture database. This
// creates tables/records, never connects to Runloom's configured storage.
func TestLiveDatabaseConnections(t *testing.T) {
	provider := os.Getenv("RUNLOOM_CONNECTOR_TEST_PROVIDER")
	if provider == "" {
		t.Skip("set RUNLOOM_CONNECTOR_TEST_PROVIDER/PORT/PASSWORD for a disposable database")
	}
	port, err := strconv.Atoi(os.Getenv("RUNLOOM_CONNECTOR_TEST_PORT"))
	require.NoError(t, err)
	c := dbnode.Connection{ID: "fixture", Name: "Disposable fixture", Provider: provider, Host: "127.0.0.1", Port: port, Database: "connector_test", Username: "connector", Password: os.Getenv("RUNLOOM_CONNECTOR_TEST_PASSWORD"), Access: "schema", Enabled: true, AuthSource: "admin"}
	require.NotEmpty(t, c.Password)
	e := dbnode.Executor{Network: dbnode.Network{Allowed: []string{net.JoinHostPort(c.Host, strconv.Itoa(port))}}}
	ctx := context.Background()
	require.NoError(t, e.Test(ctx, c))
	if provider != "mongodb" {
		_, err = e.Execute(ctx, c, dbnode.Operation{ConnectionID: c.ID, Operation: "sql", Query: "CREATE TABLE connector_records (id INT PRIMARY KEY, name VARCHAR(200))"})
		require.NoError(t, err)
	}
	op := dbnode.Operation{ConnectionID: c.ID, Table: "connector_records", Operation: "insert", Values: map[string]any{"id": 1, "name": "O'Reilly; DROP TABLE connector_records"}}
	r, err := e.Execute(ctx, c, op)
	require.NoError(t, err)
	require.EqualValues(t, 1, r.Affected)
	op.Values = map[string]any{"id": 2, "name": "two"}
	_, err = e.Execute(ctx, c, op)
	require.NoError(t, err)
	tables, err := e.Discover(ctx, c)
	require.NoError(t, err)
	found := false
	for _, table := range tables {
		if table.Name == op.Table {
			found = true
			require.GreaterOrEqual(t, len(table.Columns), 2)
			require.Equal(t, provider == "mongodb", table.Inferred)
		}
	}
	require.True(t, found)
	op.Operation = "select"
	op.Values = nil
	op.Limit = 1
	r, err = e.Execute(ctx, c, op)
	require.NoError(t, err)
	require.Len(t, r.Rows, 1)
	require.True(t, r.Truncated)
	op.Filter = map[string]any{"id": 1}
	op.Limit = 100
	r, err = e.Execute(ctx, c, op)
	require.NoError(t, err)
	require.Len(t, r.Rows, 1)
	require.Equal(t, "O'Reilly; DROP TABLE connector_records", r.Rows[0]["name"])
	op.Operation = "update"
	op.Values = map[string]any{"name": "updated"}
	read := c
	read.Access = "read"
	_, err = e.Execute(ctx, read, op)
	require.Error(t, err)
	r, err = e.Execute(ctx, c, op)
	require.NoError(t, err)
	require.EqualValues(t, 1, r.Affected)
	if provider == "mongodb" {
		r, err = e.Execute(ctx, read, dbnode.Operation{ConnectionID: c.ID, Operation: "aggregate", Table: op.Table, Pipeline: []map[string]any{{"$match": map[string]any{"id": 1}}}})
		require.NoError(t, err)
		require.Len(t, r.Rows, 1)
	} else {
		placeholder := "$1"
		if provider == "mysql" {
			placeholder = "?"
		}
		query := dbnode.Operation{ConnectionID: c.ID, Operation: "sql", Query: "SELECT name FROM connector_records WHERE id=" + placeholder, Parameters: []any{1}}
		r, err = e.Execute(ctx, read, query)
		require.NoError(t, err)
		require.Equal(t, "updated", r.Rows[0]["name"])
		if provider == "postgres" {
			supabase := c
			supabase.Provider = "supabase"
			require.NoError(t, e.Test(ctx, supabase))
			_, err = e.Discover(ctx, supabase)
			require.NoError(t, err)
			_, err = e.Execute(ctx, supabase, query)
			require.NoError(t, err)
			_, err = e.Execute(ctx, read, dbnode.Operation{ConnectionID: c.ID, Operation: "sql", Query: "SELECT 1 INTO forbidden_read_write"})
			require.Error(t, err)
		}
	}
	op.Operation = "delete"
	op.Values = nil
	r, err = e.Execute(ctx, c, op)
	require.NoError(t, err)
	require.EqualValues(t, 1, r.Affected)
	if provider == "mysql" {
		return
	} // Application storage supports PostgreSQL/MongoDB, not MySQL.
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(c.Host, fmt.Sprint(c.Port)), Path: "/" + c.Database, User: url.UserPassword(c.Username, c.Password)}
	q := u.Query()
	q.Set("sslmode", "disable")
	if provider == "mongodb" {
		u.Scheme = "mongodb"
		q = url.Values{"authSource": []string{"admin"}}
	}
	u.RawQuery = q.Encode()
	repo, err := access.Open(ctx, provider, u.String(), c.Database, t.TempDir())
	require.NoError(t, err)
	defer repo.Close(ctx)
	d := &access.Directory{Repo: repo}
	user := access.User{ID: "connector-owner", Email: "connector@example.test"}
	_, err = d.Bind(ctx, user)
	require.NoError(t, err)
	org, err := d.CreateOrg(ctx, user, "Connector fixture")
	require.NoError(t, err)
	s, _, err := repo.Read(ctx)
	require.NoError(t, err)
	team := ""
	for id := range s.Orgs[org].Teams {
		team = id
	}
	key := filepath.Join(t.TempDir(), "key")
	require.NoError(t, os.WriteFile(key, []byte(strings.Repeat("k", 32)), 0600))
	vault, err := access.NewSecretVault(d, key)
	require.NoError(t, err)
	require.NoError(t, vault.PutConnection(ctx, user, org, team, c))
	resolved, err := vault.ResolveConnection(ctx, team, c.ID)
	require.NoError(t, err)
	require.True(t, resolved.Password == c.Password)
	stored, _, err := repo.Module("secrets").ReadOwned(ctx)
	require.NoError(t, err)
	raw, err := json.Marshal(stored)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(raw), c.Password))
	require.Empty(t, stored.Orgs)
	require.Empty(t, stored.Users)
	c.Password = ""
	c.Name = "Updated metadata"
	require.NoError(t, vault.PutConnection(ctx, user, org, team, c))
	resolved, err = vault.ResolveConnection(ctx, team, c.ID)
	require.NoError(t, err)
	require.Equal(t, "Updated metadata", resolved.Name)
	require.NotEmpty(t, resolved.Password)
	_, err = vault.ResolveConnection(ctx, "other-team", c.ID)
	require.Error(t, err)
}
