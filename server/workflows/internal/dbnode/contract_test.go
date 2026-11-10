package dbnode

import (
	"context"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestBoundQueriesAndValidation(t *testing.T) {
	for _, provider := range []string{"postgres", "supabase", "mysql"} {
		c := Connection{Provider: provider, Access: "write"}
		op := Operation{ConnectionID: "orders", Operation: "update", Table: "customers", Values: map[string]any{"name": "x'; DROP TABLE customers"}, Filter: map[string]any{"id": 42, "deleted": nil}}
		query, args, err := sqlStatement(c, op)
		require.NoError(t, err)
		require.NotContains(t, query, "DROP")
		require.Contains(t, query, "IS NULL")
		require.Len(t, args, 2)
		if provider == "mysql" {
			require.Equal(t, 2, strings.Count(query, "?"))
		} else {
			require.Contains(t, query, "$1")
			require.Contains(t, query, "$2")
		}
		for _, query := range []string{"DELETE FROM customers", "SELECT 1; DELETE FROM customers", "SELECT ${input.id}"} {
			_, _, err = sqlStatement(c, Operation{ConnectionID: "orders", Operation: "sql", Query: query})
			require.Error(t, err)
		}
	}
	for _, op := range []Operation{
		{ConnectionID: "orders", Operation: "delete", Table: "customers"},
		{ConnectionID: "orders", Operation: "select", Table: "customers;DROP"},
		{ConnectionID: "orders", Operation: "select", Table: "customers", Limit: 1001},
		{ConnectionID: "orders", Operation: "aggregate", Table: "customers", Pipeline: []map[string]any{{"$out": "stolen"}}},
		{ConnectionID: "orders", Operation: "aggregate", Table: "customers", Pipeline: []map[string]any{{"$project": map[string]any{"x": map[string]any{"$function": map[string]any{"body": "evil"}}}}}},
	} {
		require.Error(t, Validate(&op))
	}
}

func TestNetworkFailsClosedAndDisabledConnections(t *testing.T) {
	n := Network{Allowed: []string{"127.0.0.1:1"}, Blocked: []string{"localhost:2"}}
	require.Error(t, n.Check("localhost", 2, true))
	require.Error(t, n.Check("example.com", 5432, false))
	for _, address := range []string{"127.0.0.1:2", "[::1]:2", "169.254.169.254:80", "100.64.0.1:1"} {
		_, err := n.DialContext(context.Background(), "tcp", address)
		require.Error(t, err)
	}
	_, err := n.DialContext(context.Background(), "unix", "/tmp/socket")
	require.Error(t, err)
	e := Executor{}
	require.ErrorContains(t, e.Test(context.Background(), Connection{}), "disabled")
	_, err = e.Discover(context.Background(), Connection{})
	require.ErrorContains(t, err, "disabled")
	_, err = e.Execute(context.Background(), Connection{}, Operation{ConnectionID: "orders", Operation: "select", Table: "orders"})
	require.ErrorContains(t, err, "disabled")
}
