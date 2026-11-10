package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/protection"
	"github.com/example/runloom/workflows/internal/recordset"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

const postgresSchema = `
CREATE TABLE IF NOT EXISTS runloom_llm_usage (
 id TEXT PRIMARY KEY, ancestors JSONB NOT NULL, status TEXT NOT NULL, body JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS runloom_llm_usage_ancestors_idx ON runloom_llm_usage USING GIN (ancestors);
CREATE INDEX IF NOT EXISTS runloom_llm_usage_started_idx ON runloom_llm_usage ((body->>'started_at'), id);
CREATE TABLE IF NOT EXISTS runloom_definitions (
  definition_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  body JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (definition_id, version)
);
CREATE TABLE IF NOT EXISTS runloom_definition_commits (
  commit_id UUID PRIMARY KEY,
  definition_id TEXT NOT NULL,
  sequence BIGINT NOT NULL,
  kind TEXT NOT NULL,
  actor TEXT NOT NULL,
  message TEXT NOT NULL,
  published_version INTEGER,
  restored_from_commit_id UUID,
  body JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  UNIQUE (definition_id, sequence)
);
CREATE INDEX IF NOT EXISTS runloom_definition_commits_history_idx
  ON runloom_definition_commits (definition_id, sequence DESC);
CREATE TABLE IF NOT EXISTS runloom_runs (
  workflow_id TEXT PRIMARY KEY,
  temporal_run_id TEXT NOT NULL,
  definition_id TEXT NOT NULL,
  definition_version INTEGER NOT NULL,
  trigger_type TEXT NOT NULL,
  input JSONB NOT NULL,
  temporal_status TEXT NOT NULL,
  state JSONB,
  output JSONB,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  completed_at TIMESTAMPTZ,
  observed_at TIMESTAMPTZ NOT NULL DEFAULT 'epoch'
);
CREATE INDEX IF NOT EXISTS runloom_runs_definition_created_idx
  ON runloom_runs (definition_id, created_at DESC);
ALTER TABLE runloom_runs ADD COLUMN IF NOT EXISTS metadata JSONB;
CREATE INDEX IF NOT EXISTS runloom_runs_created_id_idx ON runloom_runs (created_at DESC, workflow_id DESC);
CREATE INDEX IF NOT EXISTS runloom_runs_project_tag_idx ON runloom_runs ((metadata->>'project_id'), created_at DESC, workflow_id DESC);
CREATE INDEX IF NOT EXISTS runloom_runs_source_tag_idx ON runloom_runs ((metadata->>'source'), created_at DESC, workflow_id DESC);
CREATE INDEX IF NOT EXISTS runloom_runs_root_tag_idx ON runloom_runs ((metadata->>'root_execution_id'), created_at DESC, workflow_id DESC);
CREATE TABLE IF NOT EXISTS runloom_audit_events (
  event_id UUID PRIMARY KEY,
  workflow_id TEXT,
  definition_id TEXT,
  event_type TEXT NOT NULL,
  step_id TEXT,
  actor TEXT NOT NULL,
  payload JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS runloom_audit_workflow_created_idx
  ON runloom_audit_events (workflow_id, created_at DESC);
CREATE INDEX IF NOT EXISTS runloom_audit_definition_created_idx
  ON runloom_audit_events (definition_id, created_at DESC);
`

type PostgresStore struct {
	*promptStore
	*projectStore
	db *sql.DB
}

func NewPostgres(ctx context.Context, url string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	return initializePostgres(ctx, db)
}

// Each team uses its own schema. search_path intentionally excludes public.
func NewPostgresTeam(ctx context.Context, url, schema string) (*PostgresStore, error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	admin := stdlib.OpenDB(*cfg)
	defer admin.Close()
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{schema}.Sanitize()); err != nil {
		return nil, err
	}
	cfg.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*cfg)
	result, err := initializePostgres(ctx, db)
	if err == nil {
		db.SetMaxIdleConns(1)
		db.SetMaxOpenConns(4)
	}
	return result, err
}

func initializePostgres(ctx context.Context, db *sql.DB) (*PostgresStore, error) {
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetMaxIdleConns(5)
	db.SetMaxOpenConns(20)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, postgresSchema+recordset.Schema); err != nil {
		db.Close()
		return nil, err
	}
	s := &PostgresStore{db: db}
	s.projectStore = newProjectStore(s.readProjects, s.swapProjects, s.Get)
	s.promptStore = &promptStore{repo: recordset.Postgres(s.db, "prompts")}
	return s, nil
}

func (s *PostgresStore) Driver() string { return "postgres" }

func (s *PostgresStore) SaveDraft(value model.Definition) (model.Definition, error) {
	normalized, _, err := s.CommitDraft(value, CommitMeta{Actor: "system", Message: "Saved draft"})
	return normalized, err
}

func (s *PostgresStore) CommitDraft(value model.Definition, meta CommitMeta) (model.Definition, DefinitionCommit, error) {
	normalized, err := definition.Draft(value)
	if err != nil {
		return normalized, DefinitionCommit{}, err
	}
	normalized.Version = 0
	body, err := json.Marshal(normalized)
	if err != nil {
		return normalized, DefinitionCommit{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return normalized, DefinitionCommit{}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, normalized.ID); err != nil {
		return normalized, DefinitionCommit{}, err
	}
	_, err = tx.Exec(`
INSERT INTO runloom_definitions (definition_id, version, body)
VALUES ($1, 0, $2)
ON CONFLICT (definition_id, version)
DO UPDATE SET body = EXCLUDED.body, created_at = CURRENT_TIMESTAMP`, normalized.ID, body)
	if err != nil {
		return normalized, DefinitionCommit{}, err
	}
	commit, err := insertPostgresCommit(tx, normalized, meta, "save", nil, "")
	if err != nil {
		return normalized, commit, err
	}
	return normalized, commit, tx.Commit()
}

func (s *PostgresStore) Publish(id string) (model.Definition, error) {
	return s.PublishWithCommit(id, CommitMeta{Actor: "system"})
}

func (s *PostgresStore) PublishWithCommit(id string, meta CommitMeta) (model.Definition, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return model.Definition{}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, id); err != nil {
		return model.Definition{}, err
	}
	var raw []byte
	if err := tx.QueryRow(`SELECT body FROM runloom_definitions WHERE definition_id = $1 AND version = 0 FOR UPDATE`, id).Scan(&raw); errors.Is(err, sql.ErrNoRows) {
		return model.Definition{}, ErrNotFound
	} else if err != nil {
		return model.Definition{}, err
	}
	var latest int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM runloom_definitions WHERE definition_id = $1`, id).Scan(&latest); err != nil {
		return model.Definition{}, err
	}
	var value model.Definition
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, err
	}
	value.Version = latest + 1
	value, err = definition.Normalize(value)
	if err != nil {
		return value, err
	}
	body, err := json.Marshal(value)
	if err != nil {
		return value, err
	}
	if _, err := tx.Exec(`INSERT INTO runloom_definitions (definition_id, version, body) VALUES ($1, $2, $3)`, id, value.Version, body); err != nil {
		return value, err
	}
	version := value.Version
	if meta.Message == "" {
		meta.Message = fmt.Sprintf("Published version %d", version)
	}
	if _, err := insertPostgresCommit(tx, value, meta, "publish", &version, ""); err != nil {
		return value, err
	}
	return value, tx.Commit()
}

func (s *PostgresStore) Get(id string, version *int) (model.Definition, error) {
	query := `SELECT body FROM runloom_definitions WHERE definition_id = $1 AND version > 0 ORDER BY version DESC LIMIT 1`
	args := []any{id}
	if version != nil {
		query = `SELECT body FROM runloom_definitions WHERE definition_id = $1 AND version = $2`
		args = append(args, *version)
	}
	var raw []byte
	if err := s.db.QueryRow(query, args...).Scan(&raw); errors.Is(err, sql.ErrNoRows) {
		return model.Definition{}, ErrNotFound
	} else if err != nil {
		return model.Definition{}, err
	}
	var value model.Definition
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, err
	}
	return value, nil
}

func (s *PostgresStore) List() ([]Summary, error) {
	rows, err := s.db.Query(`SELECT definition_id, version, body FROM runloom_definitions ORDER BY definition_id, version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []definitionRecord{}
	for rows.Next() {
		var record definitionRecord
		var raw []byte
		if err := rows.Scan(&record.ID, &record.Version, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &record.Body); err != nil {
			return nil, err
		}
		values = append(values, record)
	}
	return summarizeDefinitions(values), rows.Err()
}

func (s *PostgresStore) ListCommits(definitionID string, limit int) ([]DefinitionCommit, error) {
	rows, err := s.db.Query(`
SELECT commit_id::text, definition_id, sequence, kind, actor, message,
       published_version, COALESCE(restored_from_commit_id::text,''), created_at
FROM runloom_definition_commits WHERE definition_id=$1
ORDER BY sequence DESC LIMIT $2`, definitionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []DefinitionCommit{}
	for rows.Next() {
		value, err := scanPostgresCommitSummary(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *PostgresStore) GetCommit(definitionID, commitID string) (DefinitionCommit, error) {
	row := s.db.QueryRow(`
SELECT commit_id::text, definition_id, sequence, kind, actor, message,
       published_version, COALESCE(restored_from_commit_id::text,''), body, created_at
FROM runloom_definition_commits WHERE definition_id=$1 AND commit_id::text=$2`, definitionID, commitID)
	value, err := scanPostgresCommit(row)
	if errors.Is(err, sql.ErrNoRows) {
		return value, ErrNotFound
	}
	return value, err
}

func (s *PostgresStore) RestoreCommit(definitionID, commitID string, meta CommitMeta) (model.Definition, DefinitionCommit, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return model.Definition{}, DefinitionCommit{}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, definitionID); err != nil {
		return model.Definition{}, DefinitionCommit{}, err
	}
	var raw []byte
	var sourceSequence int64
	if err := tx.QueryRow(`SELECT sequence, body FROM runloom_definition_commits WHERE definition_id=$1 AND commit_id::text=$2`, definitionID, commitID).Scan(&sourceSequence, &raw); errors.Is(err, sql.ErrNoRows) {
		return model.Definition{}, DefinitionCommit{}, ErrNotFound
	} else if err != nil {
		return model.Definition{}, DefinitionCommit{}, err
	}
	var value model.Definition
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, DefinitionCommit{}, err
	}
	value.Version = 0
	value, err = definition.Draft(value)
	if err != nil {
		return value, DefinitionCommit{}, err
	}
	body, err := json.Marshal(value)
	if err != nil {
		return value, DefinitionCommit{}, err
	}
	if _, err := tx.Exec(`
INSERT INTO runloom_definitions (definition_id, version, body) VALUES ($1,0,$2)
ON CONFLICT (definition_id, version)
DO UPDATE SET body=EXCLUDED.body, created_at=CURRENT_TIMESTAMP`, definitionID, body); err != nil {
		return value, DefinitionCommit{}, err
	}
	if meta.Message == "" {
		meta.Message = fmt.Sprintf("Restored commit %d", sourceSequence)
	}
	commit, err := insertPostgresCommit(tx, value, meta, "restore", nil, commitID)
	if err != nil {
		return value, commit, err
	}
	return value, commit, tx.Commit()
}

func (s *PostgresStore) RecordRunStart(value RunRecord) (RunRecord, error) {
	value = normalizeRun(value)
	metadata, err := json.Marshal(value.Metadata)
	if err != nil {
		return value, err
	}
	input, err := protection.Marshal("run/"+value.WorkflowID+"/input", value.Input)
	if err != nil {
		return value, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return value, err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
INSERT INTO runloom_runs (
 workflow_id, temporal_run_id, definition_id, definition_version, trigger_type,
 input, temporal_status, state, output, created_at, updated_at, completed_at, metadata
) VALUES ($1,$2,$3,$4,$5,$6,$7,NULL,NULL,$8,$9,NULL,$10)`,
		value.WorkflowID, value.TemporalRunID, value.DefinitionID, value.DefinitionVersion,
		value.TriggerType, input, value.TemporalStatus, value.CreatedAt, value.UpdatedAt, metadata)
	if err != nil {
		return value, err
	}
	if _, err = appendPostgresAudit(tx, AuditEvent{WorkflowID: value.WorkflowID, DefinitionID: value.DefinitionID, EventType: "run.recorded", Actor: "system"}); err != nil {
		return value, err
	}
	return value, tx.Commit()
}

func (s *PostgresStore) UpdateRun(workflowID, temporalStatus string, state *model.RunState) (bool, error) {
	return s.ObserveRun(context.Background(), workflowID, temporalStatus, state, time.Now().UTC())
}

func (s *PostgresStore) ObserveRun(ctx context.Context, workflowID, temporalStatus string, state *model.RunState, observed time.Time) (bool, error) {
	observed = observed.UTC().Truncate(time.Millisecond)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var previousObserved time.Time
	var previousStatus string
	var previousState []byte
	if err := tx.QueryRowContext(ctx, `SELECT temporal_status, state, observed_at FROM runloom_runs WHERE workflow_id = $1 FOR UPDATE`, workflowID).Scan(&previousStatus, &previousState, &previousObserved); errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	} else if err != nil {
		return false, err
	}
	if !acceptObservation(previousStatus, previousObserved, temporalStatus, observed) {
		return false, nil
	}
	var stateJSON []byte
	if state == nil && len(previousState) > 0 && string(previousState) != "null" {
		if err := protection.Unmarshal("run/"+workflowID+"/state", previousState, &state); err != nil {
			return false, err
		}
	}
	if state != nil {
		var err error
		stateJSON, err = protection.Marshal("run/"+workflowID+"/state", state)
		if err != nil {
			return false, err
		}
	}
	var prior *model.RunState
	if len(previousState) > 0 {
		if err := protection.Unmarshal("run/"+workflowID+"/state", previousState, &prior); err != nil {
			return false, err
		}
	}
	changed := previousStatus != temporalStatus || !statesEqual(prior, state)
	var output any
	if state != nil {
		output = state.Output
	}
	outputJSON, err := protection.Marshal("run/"+workflowID+"/output", output)
	if err != nil {
		return false, err
	}
	var completedAt any
	if finished(temporalStatus) {
		completedAt = utcNow()
	}
	_, err = tx.ExecContext(ctx, `
UPDATE runloom_runs SET temporal_status=$1, state=$2, output=$3, updated_at=$4,
completed_at=COALESCE(completed_at,$5), observed_at=$7 WHERE workflow_id=$6`,
		temporalStatus, nullableJSON(stateJSON, state != nil), nullableJSON(outputJSON, output != nil), utcNow(), completedAt, workflowID, observed)
	if err != nil {
		return false, err
	}
	if changed {
		if _, err = appendPostgresAudit(tx, AuditEvent{WorkflowID: workflowID, EventType: "run.observed", Actor: "system", Payload: map[string]any{"status": temporalStatus}}); err != nil {
			return false, err
		}
	}
	err = tx.Commit()
	return changed && err == nil, err
}

func (s *PostgresStore) GetRun(workflowID string) (RunRecord, error) {
	row := s.db.QueryRow(`SELECT workflow_id, temporal_run_id, definition_id, definition_version, trigger_type, input, temporal_status, state, output, created_at, updated_at, completed_at, metadata FROM runloom_runs WHERE workflow_id=$1`, workflowID)
	value, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return value, ErrNotFound
	}
	return value, err
}

func (s *PostgresStore) ListRuns(limit int) ([]RunRecord, error) {
	rows, err := s.db.Query(`SELECT workflow_id, temporal_run_id, definition_id, definition_version, trigger_type, input, temporal_status, state, output, created_at, updated_at, completed_at, metadata FROM runloom_runs ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []RunRecord{}
	for rows.Next() {
		value, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *PostgresStore) AppendAudit(value AuditEvent) (AuditEvent, error) {
	return appendPostgresAudit(s.db, value)
}

func appendPostgresAudit(db interface {
	Exec(string, ...any) (sql.Result, error)
}, value AuditEvent) (AuditEvent, error) {
	value = normalizeAudit(value)
	payload, err := protection.Marshal(auditContext(value), value.Payload)
	if err != nil {
		return value, err
	}
	// Stable event IDs deduplicate retried consumer starts; never overwrite history.
	_, err = db.Exec(`INSERT INTO runloom_audit_events (event_id,workflow_id,definition_id,event_type,step_id,actor,payload,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (event_id) DO NOTHING`,
		value.EventID, nullableString(value.WorkflowID), nullableString(value.DefinitionID), value.EventType,
		nullableString(value.StepID), value.Actor, payload, value.CreatedAt)
	return value, err
}

func (s *PostgresStore) ListAudit(workflowID, definitionID string, limit int) ([]AuditEvent, error) {
	query := `SELECT event_id::text, COALESCE(workflow_id,''), COALESCE(definition_id,''), event_type, COALESCE(step_id,''), actor, payload, created_at FROM runloom_audit_events`
	args := []any{}
	conditions := []string{}
	if workflowID != "" {
		args = append(args, workflowID)
		conditions = append(conditions, fmt.Sprintf("workflow_id=$%d", len(args)))
	}
	if definitionID != "" {
		args = append(args, definitionID)
		conditions = append(conditions, fmt.Sprintf("definition_id=$%d", len(args)))
	}
	if len(conditions) > 0 {
		query += " WHERE " + conditions[0]
		for _, condition := range conditions[1:] {
			query += " AND " + condition
		}
	}
	args = append(args, limit)
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", len(args))
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []AuditEvent{}
	for rows.Next() {
		var value AuditEvent
		var payload []byte
		var created time.Time
		if err := rows.Scan(&value.EventID, &value.WorkflowID, &value.DefinitionID, &value.EventType, &value.StepID, &value.Actor, &payload, &created); err != nil {
			return nil, err
		}
		if err := protection.Unmarshal(auditContext(value), payload, &value.Payload); err != nil {
			return nil, err
		}
		value.CreatedAt = created.UTC().Format(time.RFC3339Nano)
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *PostgresStore) Health(ctx context.Context) bool { return s.db.PingContext(ctx) == nil }
func (s *PostgresStore) Close(_ context.Context) error   { return s.db.Close() }

type rowScanner interface {
	Scan(...any) error
}

func insertPostgresCommit(tx *sql.Tx, value model.Definition, meta CommitMeta, kind string, publishedVersion *int, restoredFrom string) (DefinitionCommit, error) {
	var sequence int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(sequence),0)+1 FROM runloom_definition_commits WHERE definition_id=$1`, value.ID).Scan(&sequence); err != nil {
		return DefinitionCommit{}, err
	}
	body := value
	commit := normalizeCommit(DefinitionCommit{CommitID: uuid.NewString(), DefinitionID: value.ID, Sequence: sequence, Kind: kind, Actor: meta.Actor, Message: meta.Message, PublishedVersion: publishedVersion, RestoredFromCommitID: restoredFrom, Body: &body})
	raw, err := json.Marshal(value)
	if err != nil {
		return commit, err
	}
	_, err = tx.Exec(`
INSERT INTO runloom_definition_commits (
 commit_id, definition_id, sequence, kind, actor, message,
 published_version, restored_from_commit_id, body, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		commit.CommitID, commit.DefinitionID, commit.Sequence, commit.Kind, commit.Actor,
		commit.Message, commit.PublishedVersion, nullableString(commit.RestoredFromCommitID), raw, commit.CreatedAt)
	return commit, err
}

func scanPostgresCommitSummary(scanner rowScanner) (DefinitionCommit, error) {
	var value DefinitionCommit
	var published sql.NullInt64
	var created time.Time
	err := scanner.Scan(&value.CommitID, &value.DefinitionID, &value.Sequence, &value.Kind,
		&value.Actor, &value.Message, &published, &value.RestoredFromCommitID, &created)
	if published.Valid {
		version := int(published.Int64)
		value.PublishedVersion = &version
	}
	value.CreatedAt = created.UTC().Format(time.RFC3339Nano)
	return value, err
}

func scanPostgresCommit(scanner rowScanner) (DefinitionCommit, error) {
	var value DefinitionCommit
	var published sql.NullInt64
	var body []byte
	var created time.Time
	err := scanner.Scan(&value.CommitID, &value.DefinitionID, &value.Sequence, &value.Kind,
		&value.Actor, &value.Message, &published, &value.RestoredFromCommitID, &body, &created)
	if err != nil {
		return value, err
	}
	if published.Valid {
		version := int(published.Int64)
		value.PublishedVersion = &version
	}
	value.CreatedAt = created.UTC().Format(time.RFC3339Nano)
	value.Body = &model.Definition{}
	if err := json.Unmarshal(body, value.Body); err != nil {
		return value, err
	}
	return value, nil
}

func scanRun(scanner rowScanner) (RunRecord, error) {
	var value RunRecord
	var input, state, output, metadata []byte
	var created, updated time.Time
	var completed sql.NullTime
	err := scanner.Scan(&value.WorkflowID, &value.TemporalRunID, &value.DefinitionID, &value.DefinitionVersion,
		&value.TriggerType, &input, &value.TemporalStatus, &state, &output, &created, &updated, &completed, &metadata)
	if err != nil {
		return value, err
	}
	if err := protection.Unmarshal("run/"+value.WorkflowID+"/input", input, &value.Input); err != nil {
		return value, err
	}
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &value.Metadata); err != nil {
			return value, err
		}
	}
	if len(state) > 0 {
		value.State = &model.RunState{}
		if err := protection.Unmarshal("run/"+value.WorkflowID+"/state", state, value.State); err != nil {
			return value, err
		}
	}
	if len(output) > 0 {
		if err := protection.Unmarshal("run/"+value.WorkflowID+"/output", output, &value.Output); err != nil {
			return value, err
		}
	}
	value.CreatedAt = created.UTC().Format(time.RFC3339Nano)
	value.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
	if completed.Valid {
		formatted := completed.Time.UTC().Format(time.RFC3339Nano)
		value.CompletedAt = &formatted
	}
	return value, nil
}

type definitionRecord struct {
	ID      string
	Version int
	Body    model.Definition
}

func summarizeDefinitions(records []definitionRecord) []Summary {
	grouped := map[string][]definitionRecord{}
	for _, record := range records {
		grouped[record.ID] = append(grouped[record.ID], record)
	}
	ids := make([]string, 0, len(grouped))
	for id := range grouped {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	items := make([]Summary, 0, len(ids))
	for _, id := range ids {
		var source model.Definition
		var published model.Definition
		publishedVersion := 0
		versions := []int{}
		hasDraft := false
		for _, record := range grouped[id] {
			if record.Version == 0 {
				hasDraft = true
				source = record.Body
			} else {
				versions = append(versions, record.Version)
				if record.Version > publishedVersion {
					published = record.Body
					publishedVersion = record.Version
				}
				if !hasDraft && record.Version >= source.Version {
					source = record.Body
				}
			}
		}
		sort.Ints(versions)
		var latest *int
		if len(versions) > 0 {
			value := versions[len(versions)-1]
			latest = &value
		}
		publishedKind := published.Kind
		if latest != nil && publishedKind == "" {
			publishedKind = "main"
		}
		items = append(items, Summary{PublishedKind: publishedKind, ID: id, Name: source.Name, Description: source.Description, HasDraft: hasDraft, PublishedVersions: versions, LatestVersion: latest})
	}
	return items
}

func normalizeRun(value RunRecord) RunRecord {
	if value.CreatedAt == "" {
		value.CreatedAt = utcNow()
	}
	if value.UpdatedAt == "" {
		value.UpdatedAt = value.CreatedAt
	}
	return value
}

func normalizeAudit(value AuditEvent) AuditEvent {
	if value.EventID == "" {
		value.EventID = uuid.NewString()
	}
	if value.CreatedAt == "" {
		value.CreatedAt = utcNow()
	}
	if value.Actor == "" {
		value.Actor = "system"
	}
	if value.Payload == nil {
		value.Payload = map[string]any{}
	}
	return value
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableJSON(value []byte, present bool) any {
	if !present {
		return nil
	}
	return value
}
