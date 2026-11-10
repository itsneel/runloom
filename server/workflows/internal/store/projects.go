package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/example/runloom/workflows/internal/budget"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/recordset"
	"github.com/google/uuid"
)

const DefaultProjectID = "default"

var projectDefinitionID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

var ErrProjectNotFound = errors.New("project not found in this team")
var ErrProjectConflict = errors.New("a project with that name already exists")
var ErrProjectName = errors.New("project name must contain 1 to 80 characters, without control characters")
var ErrProjectNotEmpty = errors.New("delete or move all workflows before deleting this project")
var ErrDefaultProject = errors.New("General is the default project and cannot be deleted")

type Project struct {
	ID        string `json:"id" bson:"id"`
	Name      string `json:"name" bson:"name"`
	CreatedAt string `json:"created_at,omitempty" bson:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty" bson:"updated_at,omitempty"`
}

// Folder membership is team metadata, not part of an executable/versioned
// definition. Moving a workflow never rewrites a published snapshot or run.
type ProjectCatalog struct {
	BudgetPolicies   map[string]budget.Policy `json:"budget_policies,omitempty" bson:"budget_policies,omitempty"`
	BudgetChanges    []budget.Change          `json:"budget_changes,omitempty" bson:"budget_changes,omitempty"`
	EditLocks        map[string]EditLease     `json:"edit_locks,omitempty" bson:"edit_locks,omitempty"`
	Projects         map[string]Project       `json:"projects" bson:"projects"`
	WorkflowProjects map[string]string        `json:"workflow_projects" bson:"workflow_projects"`
}

func newProjectCatalog() ProjectCatalog {
	return ProjectCatalog{Projects: map[string]Project{DefaultProjectID: {ID: DefaultProjectID, Name: "General"}}, WorkflowProjects: map[string]string{}}
}

func newProjectScope(scope string) ProjectCatalog {
	if scope == "projects" {
		return newProjectCatalog()
	}
	return ProjectCatalog{}
}

func (c ProjectCatalog) WorkflowProject(id string) string {
	if project := c.WorkflowProjects[id]; project != "" {
		return project
	}
	return DefaultProjectID
}

type projectDocument struct {
	ID       string         `json:"id" bson:"_id"`
	Revision int64          `json:"revision" bson:"revision"`
	Body     ProjectCatalog `json:"body" bson:"body"`
}

type projectStore struct {
	read           func(context.Context, string) (ProjectCatalog, int64, error)
	swap           func(context.Context, string, int64, ProjectCatalog) (bool, error)
	getDefinition  func(string, *int) (model.Definition, error)
	policyProjects func(context.Context) (ProjectCatalog, error)
}

// ConfigurePolicyReferences supplies read-only workflow references to Management.
// Policies remain locally committed; no remote service receives database access.
func (s *projectStore) ConfigurePolicyReferences(get func(string, *int) (model.Definition, error), projects func(context.Context) (ProjectCatalog, error)) {
	s.getDefinition = get
	s.policyProjects = projects
}

func newProjectStore(read func(context.Context, string) (ProjectCatalog, int64, error), swap func(context.Context, string, int64, ProjectCatalog) (bool, error), get func(string, *int) (model.Definition, error)) *projectStore {
	return &projectStore{read: read, swap: swap, getDefinition: get}
}

func (s *projectStore) ProjectCatalog(ctx context.Context) (ProjectCatalog, error) {
	c, _, err := s.read(ctx, "projects")
	if err != nil {
		return c, err
	}
	locks, _, err := s.read(ctx, "leases")
	if err != nil {
		return c, err
	}
	c.EditLocks = locks.EditLocks
	policies, _, err := s.read(ctx, "policies")
	c.BudgetPolicies = policies.BudgetPolicies
	c.BudgetChanges = policies.BudgetChanges
	return c, err
}

// Compare-and-swap serializes metadata updates across database-backed API
// replicas, including name uniqueness checks. File storage remains single-process.
func (s *projectStore) updateProjects(ctx context.Context, change func(*ProjectCatalog) error) error {
	return s.updateScope(ctx, "projects", change)
}

func (s *projectStore) updateScope(ctx context.Context, scope string, change func(*ProjectCatalog) error) error {
	for attempt := 0; attempt < 32; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		catalog, revision, err := s.read(ctx, scope)
		if err != nil {
			return err
		}
		if err = change(&catalog); err != nil {
			return err
		}
		updated, err := s.swap(ctx, scope, revision, catalog)
		if err != nil {
			return err
		}
		if updated {
			return nil
		}
	}
	return fmt.Errorf("project catalog changed concurrently; retry")
}

func validateProjectName(c ProjectCatalog, id, name string) error {
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 80 || strings.ContainsFunc(name, unicode.IsControl) {
		return ErrProjectName
	}
	for otherID, project := range c.Projects {
		if otherID != id && strings.EqualFold(project.Name, name) {
			return ErrProjectConflict
		}
	}
	return nil
}

func (s *projectStore) CreateProject(ctx context.Context, name string) (Project, error) {
	p := Project{ID: uuid.NewString(), Name: strings.TrimSpace(name), CreatedAt: utcNow(), UpdatedAt: utcNow()}
	err := s.updateProjects(ctx, func(c *ProjectCatalog) error {
		if err := validateProjectName(*c, p.ID, p.Name); err != nil {
			return err
		}
		c.Projects[p.ID] = p
		return nil
	})
	return p, err
}

func (s *projectStore) RenameProject(ctx context.Context, id, name string) (Project, error) {
	var p Project
	err := s.updateProjects(ctx, func(c *ProjectCatalog) error {
		var ok bool
		p, ok = c.Projects[id]
		if !ok {
			return ErrProjectNotFound
		}
		name = strings.TrimSpace(name)
		if err := validateProjectName(*c, id, name); err != nil {
			return err
		}
		p.Name, p.UpdatedAt = name, utcNow()
		c.Projects[id] = p
		return nil
	})
	return p, err
}

func (s *projectStore) DeleteProject(ctx context.Context, id string) error {
	return s.updateProjects(ctx, func(c *ProjectCatalog) error {
		if _, ok := c.Projects[id]; !ok {
			return ErrProjectNotFound
		}
		if id == DefaultProjectID {
			return ErrDefaultProject
		}
		// Membership and deletion share the same CAS revision, so a concurrent
		// move cannot create a workflow assignment into a deleted project.
		for _, project := range c.WorkflowProjects {
			if project == id {
				return ErrProjectNotEmpty
			}
		}
		delete(c.Projects, id)
		return nil
	})
}

func (s *projectStore) AssignProject(ctx context.Context, id, projectID string) error {
	if !projectDefinitionID.MatchString(id) {
		return ErrNotFound
	}
	version := 0
	_, err := s.getDefinition(id, &version)
	if errors.Is(err, ErrNotFound) {
		_, err = s.getDefinition(id, nil)
	}
	if err != nil {
		return err
	}
	return s.updateProjects(ctx, func(c *ProjectCatalog) error {
		if _, ok := c.Projects[projectID]; !ok {
			return ErrProjectNotFound
		}
		c.WorkflowProjects[id] = projectID
		return nil
	})
}

func (s *Store) readProjects(_ context.Context, scope string) (ProjectCatalog, int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readProjectsLocked(scope)
}

func (s *Store) readProjectsLocked(scope string) (ProjectCatalog, int64, error) {
	d := projectDocument{Body: newProjectScope(scope)}
	err := s.readJSON(filepath.Join(s.root, "_projects", scope+".json"), &d)
	if errors.Is(err, ErrNotFound) {
		err = nil
	}
	return d.Body, d.Revision, err
}

func (s *Store) swapProjects(_ context.Context, scope string, revision int64, c ProjectCatalog) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, current, err := s.readProjectsLocked(scope)
	if err != nil || current != revision {
		return false, err
	}
	err = s.writeJSON(filepath.Join(s.root, "_projects", scope+".json"), projectDocument{ID: scope, Revision: revision + 1, Body: c})
	return err == nil, err
}

func (s *PostgresStore) readProjects(ctx context.Context, scope string) (ProjectCatalog, int64, error) {
	c := newProjectScope(scope)
	records, revision, err := recordset.Postgres(s.db, scope).Read(ctx)
	if err == nil {
		err = recordset.Decode(records, &c)
	}
	return c, revision, err
}

func (s *PostgresStore) swapProjects(ctx context.Context, scope string, revision int64, c ProjectCatalog) (bool, error) {
	records, err := recordset.Encode(c)
	if err != nil {
		return false, err
	}
	return recordset.Postgres(s.db, scope).Swap(ctx, revision, records)
}

func (s *MongoStore) readProjects(ctx context.Context, scope string) (ProjectCatalog, int64, error) {
	c := newProjectScope(scope)
	records, revision, err := recordset.Mongo(s.client, s.projects, scope).Read(ctx)
	if err == nil {
		err = recordset.Decode(records, &c)
	}
	return c, revision, err
}

func (s *MongoStore) swapProjects(ctx context.Context, scope string, revision int64, c ProjectCatalog) (bool, error) {
	records, err := recordset.Encode(c)
	if err != nil {
		return false, err
	}
	return recordset.Mongo(s.client, s.projects, scope).Swap(ctx, revision, records)
}
