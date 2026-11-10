package store

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

func Open(ctx context.Context, driver, url, databaseName, dataDir string) (Backend, error) {
	switch strings.ToLower(driver) {
	case "", "file":
		return New(dataDir)
	case "postgres", "postgresql":
		if url == "" {
			return nil, fmt.Errorf("RUNLOOM_DATABASE_URL is required for PostgreSQL")
		}
		return NewPostgres(ctx, url)
	case "mongo", "mongodb":
		if url == "" {
			return nil, fmt.Errorf("RUNLOOM_DATABASE_URL is required for MongoDB")
		}
		return NewMongo(ctx, url, databaseName)
	default:
		return nil, fmt.Errorf("RUNLOOM_DATABASE_DRIVER must be file, postgres, or mongodb")
	}
}

func OpenTeam(ctx context.Context, driver, url, databaseName, dataDir, team string) (Backend, error) {
	id, err := uuid.Parse(team)
	if err != nil || id.String() != team {
		return nil, fmt.Errorf("invalid team identifier")
	}
	prefix := "runloom_team_" + strings.ReplaceAll(team, "-", "")
	switch strings.ToLower(driver) {
	case "postgres", "postgresql":
		return NewPostgresTeam(ctx, url, prefix)
	case "mongo", "mongodb":
		return NewMongoTeam(ctx, url, databaseName, prefix+"_")
	case "", "file":
		return New(filepath.Join(dataDir, "teams", team))
	default:
		return nil, fmt.Errorf("invalid database driver")
	}
}
