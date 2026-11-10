package activities

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/dbnode"
	"go.temporal.io/sdk/temporal"
)

type Database struct {
	Vault    *access.SecretVault
	Executor dbnode.Executor
}

func (a *Database) Execute(ctx context.Context, in dbnode.Input) (any, error) {
	c, err := a.Vault.ResolveConnection(ctx, in.TeamID, in.Operation.ConnectionID)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError("database connection is unavailable for this team", "DatabaseError", nil)
	}
	result, err := a.Executor.Execute(ctx, c, in.Operation)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError(err.Error(), "DatabaseError", nil)
	}
	// Reuse the existing HTTP output redaction before persisting any activity result.
	// Normalize driver-specific BSON maps/arrays before recursive redaction.
	raw, err := json.Marshal(result.Rows)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError("database output cannot be encoded", "DatabaseError", nil)
	}
	var rows []any
	if err = json.Unmarshal(raw, &rows); err != nil {
		return nil, temporal.NewNonRetryableApplicationError("database output cannot be encoded", "DatabaseError", nil)
	}
	return redactHTTP(map[string]any{"rows": rows, "affected": result.Affected, "truncated": result.Truncated}, []string{c.Password, base64.StdEncoding.EncodeToString([]byte(c.Password))}), nil
}
