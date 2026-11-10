package api

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
)

func (s *Server) prepareRun(r *http.Request, value model.Definition, input map[string]any) (model.RunInput, error) {
	if value.Kind == "child" {
		return model.RunInput{}, fmt.Errorf("child workflows can only be started by a parent workflow")
	}
	return s.prepareRunSnapshots(r, value, input)
}

func (s *Server) prepareRunSnapshots(r *http.Request, value model.Definition, input map[string]any) (model.RunInput, error) {
	dependencies, err := definition.ResolveDependencies(value, s.modules.Catalog.Get)
	if err != nil {
		return model.RunInput{}, err
	}
	result := model.RunInput{Definition: value, Definitions: dependencies, Input: input, TeamID: r.Header.Get("X-Runloom-Team"), Actor: requestActor(r, "api-user")}
	if s.orgID != "" {
		projects, err := s.modules.Projects.ProjectCatalog(r.Context())
		if err != nil {
			return result, fmt.Errorf("project metadata unavailable")
		}
		metadata := func(d model.Definition) model.ExecutionMetadata {
			raw, _ := json.Marshal(d)
			return model.ExecutionMetadata{Namespace: s.namespace, OrgID: s.orgID, TeamID: result.TeamID, ProjectID: projects.WorkflowProject(d.ID), DefinitionID: d.ID, DefinitionVersion: d.Version, Source: "manual", SnapshotID: fmt.Sprintf("%x", sha256.Sum256(raw))}
		}
		m := metadata(value)
		result.Metadata = &m
		result.ChildMetadata = map[string]model.ExecutionMetadata{}
		for key, d := range dependencies {
			result.ChildMetadata[key] = metadata(d)
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	if len(raw) > definition.MaxDefinitionBytes {
		return result, fmt.Errorf("run input and pinned definitions exceed 1 MiB")
	}
	return result, nil
}

func tagRun(input *model.RunInput, id, source string, test bool) {
	input.Source, input.IsTest = source, test
	if input.Metadata != nil {
		input.Metadata.RootExecutionID = id
		input.Metadata.Source = source
		input.Metadata.IsTest = test
	}
}
