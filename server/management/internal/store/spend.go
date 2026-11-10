package store

import (
	"context"
	"github.com/example/runloom/management/internal/budget"
	"github.com/example/runloom/management/internal/usage"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// SpendTotal counts direct provider attempts, never parent roll-up copies.
type SpendTotal struct {
	ProjectID    string `bson:"project_id"`
	DefinitionID string `bson:"definition_id"`
	CostUSD      string `bson:"cost"`
	Requests     int    `bson:"requests"`
	Incomplete   bool   `bson:"incomplete"`
}

func (s *Store) AggregateSpend(ctx context.Context, from, before string) ([]SpendTotal, error) {
	f := usage.Filter{All: true, StartedFrom: from, StartedBefore: before, Limit: 1000}
	totals := map[[2]string]SpendTotal{}
	for {
		rows, err := s.QueryUsage(ctx, f)
		if err != nil {
			return nil, err
		}
		for _, e := range rows {
			key := [2]string{e.ProjectID, e.DefinitionID}
			t := totals[key]
			t.ProjectID, t.DefinitionID = e.ProjectID, e.DefinitionID
			t.Requests++
			if e.Accounting == "estimated" && e.CostUSD != "" {
				t.CostUSD = budget.Money(t.CostUSD).Add(budget.Money(t.CostUSD), budget.Money(e.CostUSD)).FloatString(12)
			} else {
				t.Incomplete = true
			}
			totals[key] = t
		}
		if len(rows) < f.Limit {
			break
		}
		f.After = rows[len(rows)-1].ID
	}
	result := []SpendTotal{}
	for _, t := range totals {
		result = append(result, t)
	}
	return result, ctx.Err()
}

func (s *PostgresStore) AggregateSpend(ctx context.Context, from, before string) ([]SpendTotal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT COALESCE(body->>'project_id',''), COALESCE(body->>'definition_id',''),
 COALESCE(SUM(CASE WHEN body->>'accounting'='estimated' AND COALESCE(body->>'cost_usd','')<>'' THEN (body->>'cost_usd')::numeric ELSE 0 END),0)::text,
 COUNT(*), BOOL_OR(COALESCE(body->>'accounting','')<>'estimated' OR COALESCE(body->>'cost_usd','')='')
 FROM runloom_llm_usage WHERE body->>'started_at'>=$1 AND body->>'started_at'<$2 GROUP BY 1,2`, from, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []SpendTotal{}
	for rows.Next() {
		var t SpendTotal
		if err := rows.Scan(&t.ProjectID, &t.DefinitionID, &t.CostUSD, &t.Requests, &t.Incomplete); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

func (s *MongoStore) AggregateSpend(ctx context.Context, from, before string) ([]SpendTotal, error) {
	known := bson.M{"$and": bson.A{bson.M{"$eq": bson.A{"$body.accounting", "estimated"}}, bson.M{"$ne": bson.A{bson.M{"$ifNull": bson.A{"$body.cost_usd", ""}}, ""}}}}
	pipe := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"body.started_at": bson.M{"$gte": from, "$lt": before}}}},
		{{Key: "$group", Value: bson.M{
			"_id":      bson.M{"project_id": bson.M{"$ifNull": bson.A{"$body.project_id", ""}}, "definition_id": bson.M{"$ifNull": bson.A{"$body.definition_id", ""}}},
			"cost":     bson.M{"$sum": bson.M{"$cond": bson.A{known, bson.M{"$toDecimal": "$body.cost_usd"}, 0}}},
			"requests": bson.M{"$sum": 1}, "incomplete": bson.M{"$max": bson.M{"$not": bson.A{known}}},
		}}},
		{{Key: "$project", Value: bson.M{"_id": 0, "project_id": "$_id.project_id", "definition_id": "$_id.definition_id", "cost": bson.M{"$toString": "$cost"}, "requests": 1, "incomplete": 1}}},
	}
	cursor, err := s.usage.Aggregate(ctx, pipe)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	result := []SpendTotal{}
	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}
	return result, nil
}
