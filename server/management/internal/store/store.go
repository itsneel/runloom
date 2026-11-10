package store

import (
	"context"
	"encoding/json"
	"errors"

	"os"
	"path/filepath"

	"sync"
	"time"

	"github.com/example/runloom/management/internal/model"
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

type Store struct {
	*promptStore
	*projectStore
	root string
	mu   sync.RWMutex
}

// Backend contains only Management-owned storage operations.
type Backend interface {
	Prompts
	Metering
	Policies
	Lifecycle
	ConfigurePolicyReferences(func(string, *int) (model.Definition, error), func(context.Context) (ProjectCatalog, error))
}
type View interface {
	Backend
	Get(string, *int) (model.Definition, error)
	List() ([]Summary, error)
	ListAudit(string, string, int) ([]AuditEvent, error)
}

func New(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	s := &Store{root: root}
	s.promptStore = &promptStore{repo: promptFile{s}}
	s.projectStore = newProjectStore(s.readProjects, s.swapProjects, nil)
	return s, nil
}

func (s *Store) Driver() string { return "file" }

func (s *Store) Health(_ context.Context) bool {
	info, err := os.Stat(s.root)
	return err == nil && info.IsDir()
}

func (s *Store) Close(_ context.Context) error { return nil }

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
