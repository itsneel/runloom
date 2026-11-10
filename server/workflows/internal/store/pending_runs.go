package store

import (
	"context"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var terminalStatuses = []string{"completed", "failed", "canceled", "cancelled", "terminated", "timed_out", "timedout", "continued_as_new", "continuedasnew"}

func acceptObservation(previous string, previousAt time.Time, status string, observed time.Time) bool {
	return observed.After(previousAt) && (!finished(previous) || previous == status)
}

func pageLimit(limit int) int {
	if limit < 1 || limit > 1000 {
		return 100
	}
	return limit
}

func (s *Store) PendingRuns(ctx context.Context, after string, limit int) ([]RunRecord, error) {
	rows, err := s.ListRuns(int(^uint(0) >> 1))
	if err != nil {
		return nil, err
	}
	result := []RunRecord{}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if row.WorkflowID > after && !finished(row.TemporalStatus) {
			result = append(result, row)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].WorkflowID < result[j].WorkflowID })
	return result[:min(len(result), pageLimit(limit))], nil
}

func (s *PostgresStore) PendingRuns(ctx context.Context, after string, limit int) ([]RunRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT workflow_id, temporal_run_id, definition_id, definition_version, trigger_type, input, temporal_status, state, output, created_at, updated_at, completed_at, metadata FROM runloom_runs WHERE workflow_id > $1 AND NOT (temporal_status = ANY($2)) ORDER BY workflow_id LIMIT $3`, after, terminalStatuses, pageLimit(limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []RunRecord{}
	for rows.Next() {
		row, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *MongoStore) PendingRuns(ctx context.Context, after string, limit int) ([]RunRecord, error) {
	cursor, err := s.runs.Find(ctx, bson.M{"workflow_id": bson.M{"$gt": after}, "temporal_status": bson.M{"$nin": terminalStatuses}}, options.Find().SetSort(bson.D{{Key: "workflow_id", Value: 1}}).SetLimit(int64(pageLimit(limit))))
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
		var row RunRecord
		if err := readProtectedRun(doc, &row); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, cursor.Err()
}
