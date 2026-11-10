package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/usage"
	"github.com/google/uuid"
)

var ErrNotFound = errors.New("definition not found")

type Summary struct {
	PublishedKind     string `json:"published_kind,omitempty"`
	ProjectID         string `json:"project_id"`
	ID                string `json:"id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	HasDraft          bool   `json:"has_draft"`
	PublishedVersions []int  `json:"published_versions"`
	LatestVersion     *int   `json:"latest_version"`
}

type RunRecord struct {
	Metadata          *model.ExecutionMetadata `json:"metadata,omitempty" bson:"metadata,omitempty"`
	ObservedAt        time.Time                `json:"observed_at,omitempty" bson:"observed_at,omitempty"`
	LLMCost           *usage.Summary           `json:"llm_cost,omitempty" bson:"-"`
	WorkflowID        string                   `json:"workflow_id" bson:"workflow_id"`
	TemporalRunID     string                   `json:"temporal_run_id" bson:"temporal_run_id"`
	DefinitionID      string                   `json:"definition_id" bson:"definition_id"`
	DefinitionVersion int                      `json:"definition_version" bson:"definition_version"`
	TriggerType       string                   `json:"trigger_type" bson:"trigger_type"`
	Input             map[string]any           `json:"input" bson:"input"`
	TemporalStatus    string                   `json:"temporal_status" bson:"temporal_status"`
	State             *model.RunState          `json:"state" bson:"state"`
	Output            any                      `json:"output" bson:"output"`
	CreatedAt         string                   `json:"created_at" bson:"created_at"`
	UpdatedAt         string                   `json:"updated_at" bson:"updated_at"`
	CompletedAt       *string                  `json:"completed_at" bson:"completed_at"`
}

type AuditEvent struct {
	EventID      string `json:"event_id" bson:"event_id"`
	WorkflowID   string `json:"workflow_id,omitempty" bson:"workflow_id,omitempty"`
	DefinitionID string `json:"definition_id,omitempty" bson:"definition_id,omitempty"`
	EventType    string `json:"event_type" bson:"event_type"`
	StepID       string `json:"step_id,omitempty" bson:"step_id,omitempty"`
	Actor        string `json:"actor" bson:"actor"`
	Payload      any    `json:"payload" bson:"payload"`
	CreatedAt    string `json:"created_at" bson:"created_at"`
}

type CommitMeta struct {
	Actor   string
	Message string
}

type DefinitionCommit struct {
	CommitID             string            `json:"commit_id" bson:"commit_id"`
	DefinitionID         string            `json:"definition_id" bson:"definition_id"`
	Sequence             int64             `json:"sequence" bson:"sequence"`
	Kind                 string            `json:"kind" bson:"kind"`
	Actor                string            `json:"actor" bson:"actor"`
	Message              string            `json:"message" bson:"message"`
	PublishedVersion     *int              `json:"published_version" bson:"published_version"`
	RestoredFromCommitID string            `json:"restored_from_commit_id,omitempty" bson:"restored_from_commit_id,omitempty"`
	CreatedAt            string            `json:"created_at" bson:"created_at"`
	Body                 *model.Definition `json:"body,omitempty" bson:"body,omitempty"`
}

// Backend is a composition-root convenience, not an application-service dependency.
type Backend interface {
	Prompts
	Catalog
	Projects
	Runs
	Audits
	Metering
	Policies
	Lifecycle
}

type Store struct {
	*promptStore
	*projectStore
	root string
	mu   sync.RWMutex
}

func New(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	s := &Store{root: root}
	s.promptStore = &promptStore{repo: promptFile{s}}
	s.projectStore = newProjectStore(s.readProjects, s.swapProjects, s.Get)
	return s, nil
}

func (s *Store) Driver() string { return "file" }

func (s *Store) SaveDraft(value model.Definition) (model.Definition, error) {
	normalized, _, err := s.CommitDraft(value, CommitMeta{Actor: "system", Message: "Saved draft"})
	return normalized, err
}

func (s *Store) CommitDraft(value model.Definition, meta CommitMeta) (model.Definition, DefinitionCommit, error) {
	normalized, err := definition.Draft(value)
	if err != nil {
		return normalized, DefinitionCommit{}, err
	}
	normalized.Version = 0
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.write(s.draftPath(normalized.ID), normalized); err != nil {
		return normalized, DefinitionCommit{}, err
	}
	commit, err := s.createCommitLocked(normalized, meta, "save", nil, "")
	return normalized, commit, err
}

func (s *Store) Publish(id string) (model.Definition, error) {
	return s.PublishWithCommit(id, CommitMeta{Actor: "system"})
}

func (s *Store) PublishWithCommit(id string, meta CommitMeta) (model.Definition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	draft, err := s.read(s.draftPath(id))
	if err != nil {
		return draft, err
	}
	versions, err := s.versions(id)
	if err != nil {
		return draft, err
	}
	next := 1
	if len(versions) > 0 {
		next = versions[len(versions)-1] + 1
	}
	draft.Version = next
	normalized, err := definition.Normalize(draft)
	if err != nil {
		return normalized, err
	}
	path := filepath.Join(s.definitionDir(id), fmt.Sprintf("v%d.json", next))
	if _, err := os.Stat(path); err == nil {
		return normalized, fmt.Errorf("published versions are immutable")
	}
	if err := s.write(path, normalized); err != nil {
		return normalized, err
	}
	version := normalized.Version
	if meta.Message == "" {
		meta.Message = fmt.Sprintf("Published version %d", version)
	}
	_, err = s.createCommitLocked(normalized, meta, "publish", &version, "")
	return normalized, err
}

func (s *Store) Get(id string, version *int) (model.Definition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if version != nil && *version == 0 {
		return s.read(s.draftPath(id))
	}
	selected := 0
	if version != nil {
		selected = *version
	} else {
		versions, err := s.versions(id)
		if err != nil {
			return model.Definition{}, err
		}
		if len(versions) == 0 {
			return model.Definition{}, ErrNotFound
		}
		selected = versions[len(versions)-1]
	}
	return s.read(filepath.Join(s.definitionDir(id), fmt.Sprintf("v%d.json", selected)))
}

func (s *Store) List() ([]Summary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	items := make([]Summary, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		id := entry.Name()
		versions, err := s.versions(id)
		if err != nil {
			return nil, err
		}
		draftPath := s.draftPath(id)
		_, draftErr := os.Stat(draftPath)
		hasDraft := draftErr == nil
		var value model.Definition
		if hasDraft {
			value, err = s.read(draftPath)
		} else if len(versions) > 0 {
			value, err = s.read(filepath.Join(s.definitionDir(id), fmt.Sprintf("v%d.json", versions[len(versions)-1])))
		} else {
			continue
		}
		if err != nil {
			return nil, err
		}
		var latest *int
		publishedKind := ""
		if len(versions) > 0 {
			value := versions[len(versions)-1]
			latest = &value
			published, readErr := s.read(filepath.Join(s.definitionDir(id), fmt.Sprintf("v%d.json", value)))
			if readErr != nil {
				return nil, readErr
			}
			publishedKind = published.Kind
			if publishedKind == "" {
				publishedKind = "main"
			}
		}
		items = append(items, Summary{PublishedKind: publishedKind, ID: value.ID, Name: value.Name, Description: value.Description, HasDraft: hasDraft, PublishedVersions: versions, LatestVersion: latest})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (s *Store) ListCommits(definitionID string, limit int) ([]DefinitionCommit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	directory := s.commitDir(definitionID)
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []DefinitionCommit{}, nil
	}
	if err != nil {
		return nil, err
	}
	values := make([]DefinitionCommit, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		var value DefinitionCommit
		if err := s.readJSON(filepath.Join(directory, entry.Name()), &value); err != nil {
			return nil, err
		}
		value.Body = nil
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Sequence > values[j].Sequence })
	if len(values) > limit {
		values = values[:limit]
	}
	return values, nil
}

func (s *Store) GetCommit(definitionID, commitID string) (DefinitionCommit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getCommitLocked(definitionID, commitID)
}

func (s *Store) RestoreCommit(definitionID, commitID string, meta CommitMeta) (model.Definition, DefinitionCommit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	source, err := s.getCommitLocked(definitionID, commitID)
	if err != nil {
		return model.Definition{}, DefinitionCommit{}, err
	}
	if source.Body == nil {
		return model.Definition{}, DefinitionCommit{}, fmt.Errorf("commit body is missing")
	}
	value := *source.Body
	value.Version = 0
	value, err = definition.Draft(value)
	if err != nil {
		return value, DefinitionCommit{}, err
	}
	if err := s.write(s.draftPath(definitionID), value); err != nil {
		return value, DefinitionCommit{}, err
	}
	if meta.Message == "" {
		meta.Message = fmt.Sprintf("Restored commit %d", source.Sequence)
	}
	commit, err := s.createCommitLocked(value, meta, "restore", nil, commitID)
	return value, commit, err
}

func (s *Store) RecordRunStart(value RunRecord) (RunRecord, error) {
	now := utcNow()
	if value.CreatedAt == "" {
		value.CreatedAt = now
	}
	if value.UpdatedAt == "" {
		value.UpdatedAt = value.CreatedAt
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.runPath(value.WorkflowID)
	if err != nil {
		return value, err
	}
	if _, err := os.Stat(path); err == nil {
		return value, fmt.Errorf("workflow run already exists")
	}
	return value, s.writeJSON(path, value)
}

func (s *Store) UpdateRun(workflowID, temporalStatus string, state *model.RunState) (bool, error) {
	return s.ObserveRun(context.Background(), workflowID, temporalStatus, state, time.Now().UTC())
}

func (s *Store) ObserveRun(ctx context.Context, workflowID, temporalStatus string, state *model.RunState, observed time.Time) (bool, error) {
	observed = observed.UTC().Truncate(time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.runPath(workflowID)
	if err != nil {
		return false, err
	}
	var value RunRecord
	if err := s.readJSON(path, &value); err != nil {
		return false, err
	}
	if !acceptObservation(value.TemporalStatus, value.ObservedAt, temporalStatus, observed) {
		return false, nil
	}
	if state == nil {
		state = value.State
	}
	changed := value.TemporalStatus != temporalStatus || !reflect.DeepEqual(value.State, state)
	value.ObservedAt = observed
	value.TemporalStatus = temporalStatus
	value.State = state
	if state != nil {
		value.Output = state.Output
	} else {
		value.Output = nil
	}
	value.UpdatedAt = utcNow()
	if finished(temporalStatus) && value.CompletedAt == nil {
		completedAt := value.UpdatedAt
		value.CompletedAt = &completedAt
	}
	return changed, s.writeJSON(path, value)
}

func (s *Store) GetRun(workflowID string) (RunRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path, err := s.runPath(workflowID)
	if err != nil {
		return RunRecord{}, err
	}
	var value RunRecord
	if err := s.readJSON(path, &value); err != nil {
		return value, err
	}
	return value, nil
}

func (s *Store) ListRuns(limit int) ([]RunRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	directory := filepath.Join(s.root, "_runs")
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []RunRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	values := make([]RunRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		var value RunRecord
		if err := s.readJSON(filepath.Join(directory, entry.Name()), &value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].CreatedAt > values[j].CreatedAt })
	if len(values) > limit {
		values = values[:limit]
	}
	return values, nil
}

func (s *Store) AppendAudit(value AuditEvent) (AuditEvent, error) {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.root, "_audits", value.EventID+".json")
	if _, err := os.Stat(path); err == nil {
		return value, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return value, err
	}
	return value, s.writeJSON(path, value)
}

func (s *Store) ListAudit(workflowID, definitionID string, limit int) ([]AuditEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	directory := filepath.Join(s.root, "_audits")
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []AuditEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	values := make([]AuditEvent, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		var value AuditEvent
		if err := s.readJSON(filepath.Join(directory, entry.Name()), &value); err != nil {
			return nil, err
		}
		if workflowID != "" && value.WorkflowID != workflowID {
			continue
		}
		if definitionID != "" && value.DefinitionID != definitionID {
			continue
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].CreatedAt > values[j].CreatedAt })
	if len(values) > limit {
		values = values[:limit]
	}
	return values, nil
}

func (s *Store) Health(_ context.Context) bool {
	info, err := os.Stat(s.root)
	return err == nil && info.IsDir()
}

func (s *Store) Close(_ context.Context) error { return nil }

func (s *Store) definitionDir(id string) string {
	for _, char := range id {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
			return filepath.Join(s.root, "__invalid__")
		}
	}
	if id == "" {
		return filepath.Join(s.root, "__invalid__")
	}
	return filepath.Join(s.root, id)
}

func (s *Store) draftPath(id string) string {
	return filepath.Join(s.definitionDir(id), "draft.json")
}

func (s *Store) runPath(workflowID string) (string, error) {
	if workflowID == "" || len(workflowID) > 200 || strings.ContainsAny(workflowID, `/\\`) {
		return "", fmt.Errorf("invalid workflow id")
	}
	return filepath.Join(s.root, "_runs", workflowID+".json"), nil
}

func (s *Store) commitDir(definitionID string) string {
	return filepath.Join(s.root, "_commits", filepath.Base(s.definitionDir(definitionID)))
}

func (s *Store) createCommitLocked(value model.Definition, meta CommitMeta, kind string, publishedVersion *int, restoredFrom string) (DefinitionCommit, error) {
	directory := s.commitDir(value.ID)
	entries, err := os.ReadDir(directory)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return DefinitionCommit{}, err
	}
	var sequence int64 = 1
	for _, entry := range entries {
		prefix := strings.SplitN(entry.Name(), "-", 2)[0]
		if parsed, parseErr := strconv.ParseInt(prefix, 10, 64); parseErr == nil && parsed >= sequence {
			sequence = parsed + 1
		}
	}
	body := value
	commit := normalizeCommit(DefinitionCommit{CommitID: uuid.NewString(), DefinitionID: value.ID, Sequence: sequence, Kind: kind, Actor: meta.Actor, Message: meta.Message, PublishedVersion: publishedVersion, RestoredFromCommitID: restoredFrom, Body: &body})
	path := filepath.Join(directory, fmt.Sprintf("%020d-%s.json", sequence, commit.CommitID))
	return commit, s.writeJSON(path, commit)
}

func (s *Store) getCommitLocked(definitionID, commitID string) (DefinitionCommit, error) {
	entries, err := os.ReadDir(s.commitDir(definitionID))
	if errors.Is(err, os.ErrNotExist) {
		return DefinitionCommit{}, ErrNotFound
	}
	if err != nil {
		return DefinitionCommit{}, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		var value DefinitionCommit
		if err := s.readJSON(filepath.Join(s.commitDir(definitionID), entry.Name()), &value); err != nil {
			return DefinitionCommit{}, err
		}
		if value.CommitID == commitID {
			return value, nil
		}
	}
	return DefinitionCommit{}, ErrNotFound
}

func (s *Store) versions(id string) ([]int, error) {
	entries, err := os.ReadDir(s.definitionDir(id))
	if errors.Is(err, os.ErrNotExist) {
		return []int{}, nil
	}
	if err != nil {
		return nil, err
	}
	versions := []int{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "v") || !strings.HasSuffix(name, ".json") {
			continue
		}
		value, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "v"), ".json"))
		if err == nil {
			versions = append(versions, value)
		}
	}
	sort.Ints(versions)
	return versions, nil
}

func (s *Store) read(path string) (model.Definition, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return model.Definition{}, ErrNotFound
	}
	if err != nil {
		return model.Definition{}, err
	}
	defer file.Close()
	var value model.Definition
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}
	return value, nil
}

func (s *Store) write(path string, value model.Definition) error {
	return s.writeJSON(path, value)
}

func (s *Store) writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".runloom-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func (s *Store) readJSON(path string, target any) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewDecoder(file).Decode(target)
}

func utcNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func normalizeCommit(value DefinitionCommit) DefinitionCommit {
	if value.Actor == "" {
		value.Actor = "system"
	}
	if value.Message == "" {
		value.Message = "Updated workflow"
	}
	if len(value.Message) > 500 {
		value.Message = value.Message[:500]
	}
	if value.CreatedAt == "" {
		value.CreatedAt = utcNow()
	}
	return value
}

func finished(status string) bool {
	switch status {
	case "completed", "failed", "canceled", "cancelled", "terminated", "timed_out":
		return true
	default:
		return false
	}
}
