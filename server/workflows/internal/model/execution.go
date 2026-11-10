package model

import "go.temporal.io/sdk/temporal"

// ExecutionMetadata contains only non-sensitive, server-assigned identifiers.
// Temporal Search Attributes are plaintext even with encrypted history payloads.
type ExecutionMetadata struct {
	Namespace         string `json:"namespace"`
	OrgID             string `json:"org_id"`
	TeamID            string `json:"team_id"`
	ProjectID         string `json:"project_id"`
	DefinitionID      string `json:"definition_id"`
	DefinitionVersion int    `json:"definition_version"`
	IsTest            bool   `json:"is_test"`
	Source            string `json:"source"`
	RootExecutionID   string `json:"root_execution_id"`
	SnapshotID        string `json:"snapshot_id"`
}

func (m ExecutionMetadata) SearchUpdates() []temporal.SearchAttributeUpdate {
	return []temporal.SearchAttributeUpdate{
		temporal.NewSearchAttributeKeyKeyword("RunloomOrgId").ValueSet(m.OrgID),
		temporal.NewSearchAttributeKeyKeyword("RunloomTeamId").ValueSet(m.TeamID),
		temporal.NewSearchAttributeKeyKeyword("RunloomProjectId").ValueSet(m.ProjectID),
		temporal.NewSearchAttributeKeyKeyword("RunloomDefinitionId").ValueSet(m.DefinitionID),
		temporal.NewSearchAttributeKeyInt64("RunloomDefinitionVersion").ValueSet(int64(m.DefinitionVersion)),
		temporal.NewSearchAttributeKeyBool("RunloomIsTest").ValueSet(m.IsTest),
		temporal.NewSearchAttributeKeyKeyword("RunloomSource").ValueSet(m.Source),
		temporal.NewSearchAttributeKeyKeyword("RunloomRootExecutionId").ValueSet(m.RootExecutionID),
		temporal.NewSearchAttributeKeyKeyword("RunloomSnapshotId").ValueSet(m.SnapshotID),
	}
}

func (m *ExecutionMetadata) SearchAttributes() temporal.SearchAttributes {
	if m == nil {
		return temporal.NewSearchAttributes()
	}
	return temporal.NewSearchAttributes(m.SearchUpdates()...)
}
