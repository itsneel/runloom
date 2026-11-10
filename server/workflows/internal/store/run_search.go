package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// RunFilter searches immutable, non-sensitive execution metadata, never payloads.
// Stores are already team scoped; these filters cannot broaden authorization.
type RunFilter struct {
	Search, TagKey, TagValue string
	Limit, Offset            int
}

var runTagKeys = []string{"namespace", "org_id", "team_id", "project_id", "definition_id", "definition_version", "is_test", "source", "root_execution_id", "snapshot_id"}

func (f RunFilter) Validate() error {
	if f.Limit < 1 || f.Limit > 200 || f.Offset < 0 || f.Offset > 1000000 || len(f.Search) > 256 || len(f.TagValue) > 256 {
		return errors.New("invalid run search: limit 1–200, offset 0–1000000, search and tag value at most 256 bytes")
	}
	if (f.TagKey == "") != (f.TagValue == "") {
		return errors.New("tag_key and tag_value must be supplied together")
	}
	if f.TagKey == "" {
		return nil
	}
	valid := false
	for _, key := range runTagKeys {
		valid = valid || key == f.TagKey
	}
	if !valid {
		return errors.New("unsupported run tag key")
	}
	if f.TagKey == "is_test" && f.TagValue != "true" && f.TagValue != "false" {
		return errors.New("is_test must be true or false")
	}
	if f.TagKey == "definition_version" {
		n, err := strconv.Atoi(f.TagValue)
		if err != nil || n < 0 || strconv.Itoa(n) != f.TagValue {
			return errors.New("definition_version must be a non-negative integer")
		}
	}
	return nil
}

func runTags(r RunRecord) map[string]string {
	if r.Metadata == nil {
		return nil
	}
	m := r.Metadata
	return map[string]string{"namespace": m.Namespace, "org_id": m.OrgID, "team_id": m.TeamID, "project_id": m.ProjectID, "definition_id": m.DefinitionID, "definition_version": strconv.Itoa(m.DefinitionVersion), "is_test": strconv.FormatBool(m.IsTest), "source": m.Source, "root_execution_id": m.RootExecutionID, "snapshot_id": m.SnapshotID}
}

func (f RunFilter) matches(r RunRecord) bool {
	tags := runTags(r)
	if f.TagKey != "" && tags[f.TagKey] != f.TagValue {
		return false
	}
	q := strings.ToLower(f.Search)
	if q == "" || strings.Contains(strings.ToLower(r.WorkflowID), q) || strings.Contains(strings.ToLower(r.DefinitionID), q) {
		return true
	}
	for _, v := range tags {
		if strings.Contains(strings.ToLower(v), q) {
			return true
		}
	}
	return false
}

func (s *Store) SearchRuns(ctx context.Context, f RunFilter) ([]RunRecord, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	rows, err := s.ListRuns(int(^uint(0) >> 1))
	if err != nil {
		return nil, err
	}
	result := []RunRecord{}
	for _, r := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if f.matches(r) {
			result = append(result, r)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt == result[j].CreatedAt {
			return result[i].WorkflowID > result[j].WorkflowID
		}
		return result[i].CreatedAt > result[j].CreatedAt
	})
	start := min(f.Offset, len(result))
	return result[start:min(start+f.Limit, len(result))], nil
}

func (s *PostgresStore) SearchRuns(ctx context.Context, f RunFilter) ([]RunRecord, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	conditions := []string{"TRUE"}
	args := []any{}
	if f.Search != "" {
		args = append(args, strings.ToLower(f.Search))
		fields := []string{"workflow_id", "definition_id"}
		for _, key := range runTagKeys {
			fields = append(fields, "metadata->>'"+key+"'")
		}
		parts := []string{}
		for _, field := range fields {
			parts = append(parts, "strpos(lower(COALESCE("+field+",'')), $1) > 0")
		}
		conditions = append(conditions, "("+strings.Join(parts, " OR ")+")")
	}
	if f.TagKey != "" {
		args = append(args, f.TagValue)
		conditions = append(conditions, fmt.Sprintf("metadata->>'%s' = $%d", f.TagKey, len(args)))
	}
	args = append(args, f.Limit, f.Offset)
	query := `SELECT workflow_id, temporal_run_id, definition_id, definition_version, trigger_type, input, temporal_status, state, output, created_at, updated_at, completed_at, metadata FROM runloom_runs WHERE ` + strings.Join(conditions, " AND ") + fmt.Sprintf(" ORDER BY created_at DESC, workflow_id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []RunRecord{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *MongoStore) SearchRuns(ctx context.Context, f RunFilter) ([]RunRecord, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	filter := bson.M{}
	if f.Search != "" {
		pattern := bson.Regex{Pattern: regexp.QuoteMeta(f.Search), Options: "i"}
		parts := bson.A{bson.M{"workflow_id": pattern}, bson.M{"definition_id": pattern}}
		for _, key := range runTagKeys {
			if key == "definition_version" || key == "is_test" {
				parts = append(parts, bson.M{"$expr": bson.M{"$regexMatch": bson.M{"input": bson.M{"$convert": bson.M{"input": "$metadata." + key, "to": "string", "onNull": "", "onError": ""}}, "regex": regexp.QuoteMeta(f.Search), "options": "i"}}})
			} else {
				parts = append(parts, bson.M{"metadata." + key: pattern})
			}
		}
		filter["$or"] = parts
	}
	if f.TagKey != "" {
		var value any = f.TagValue
		if f.TagKey == "is_test" {
			value = f.TagValue == "true"
		}
		if f.TagKey == "definition_version" {
			value, _ = strconv.Atoi(f.TagValue)
		}
		filter["metadata."+f.TagKey] = value
	}
	cursor, err := s.runs.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "workflow_id", Value: -1}}).SetSkip(int64(f.Offset)).SetLimit(int64(f.Limit)))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	result := []RunRecord{}
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		var r RunRecord
		if err := readProtectedRun(doc, &r); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, cursor.Err()
}
