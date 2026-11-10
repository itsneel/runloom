package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/example/runloom/workflows/internal/usage"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func checkUsage(e usage.Entry) error {
	if _, err := uuid.Parse(e.ID); err != nil {
		return fmt.Errorf("invalid usage ID")
	}
	return nil
}
func usageLimit(f usage.Filter) int {
	if f.Limit < 1 {
		return 51
	}
	if f.Limit > 10001 {
		return 10001
	}
	return f.Limit
}

var usageFileMu sync.Mutex

func (s *Store) BeginUsage(ctx context.Context, e usage.Entry) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := checkUsage(e); err != nil {
		return false, err
	}
	usageFileMu.Lock()
	defer usageFileMu.Unlock()
	dir := filepath.Join(s.root, "llm-usage")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return false, err
	}
	file, err := os.OpenFile(filepath.Join(dir, e.ID+".json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	if err = json.NewEncoder(file).Encode(e); err == nil {
		err = file.Sync()
	}
	return err == nil, err
}
func (s *Store) FinishUsage(ctx context.Context, e usage.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkUsage(e); err != nil {
		return err
	}
	usageFileMu.Lock()
	defer usageFileMu.Unlock()
	path := filepath.Join(s.root, "llm-usage", e.ID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var previous usage.Entry
	if err = json.Unmarshal(raw, &previous); err != nil {
		return err
	}
	if previous.Status != "pending" {
		return nil
	}
	return s.writeJSON(path, e)
}
func (s *Store) QueryUsage(ctx context.Context, f usage.Filter) ([]usage.Entry, error) {
	usageFileMu.Lock()
	defer usageFileMu.Unlock()
	result := []usage.Entry{}
	files, err := os.ReadDir(filepath.Join(s.root, "llm-usage"))
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if filepath.Ext(file.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.root, "llm-usage", file.Name()))
		if err != nil {
			return nil, err
		}
		var entry usage.Entry
		if err = json.Unmarshal(raw, &entry); err != nil {
			return nil, err
		}
		if f.Match(entry) {
			result = append(result, entry)
		}
		if len(result) >= usageLimit(f) {
			break
		}
	}
	return result, nil
}

func (s *PostgresStore) BeginUsage(ctx context.Context, e usage.Entry) (bool, error) {
	if err := checkUsage(e); err != nil {
		return false, err
	}
	body, err := json.Marshal(e)
	if err != nil {
		return false, err
	}
	ancestors, _ := json.Marshal(e.Ancestors)
	result, err := s.db.ExecContext(ctx, `INSERT INTO runloom_llm_usage(id,ancestors,status,body) VALUES($1,$2,'pending',$3) ON CONFLICT(id) DO NOTHING`, e.ID, ancestors, body)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
func (s *PostgresStore) FinishUsage(ctx context.Context, e usage.Entry) error {
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE runloom_llm_usage SET body=$2,status=$3 WHERE id=$1 AND status='pending'`, e.ID, body, e.Status)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil || n > 0 {
		return err
	}
	var exists bool
	if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runloom_llm_usage WHERE id=$1)`, e.ID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}
func (s *PostgresStore) QueryUsage(ctx context.Context, f usage.Filter) ([]usage.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT body FROM runloom_llm_usage WHERE ($6 OR ancestors ? $1) AND id>$2 AND ($3='' OR body->>'model'=$3) AND ($4='' OR body->>'step_id'=$4) AND ($7='' OR body->>'started_at'>=$7) AND ($8='' OR body->>'started_at'<$8) ORDER BY id LIMIT $5`, f.RunKey, f.After, f.Model, f.StepID, usageLimit(f), f.All, f.StartedFrom, f.StartedBefore)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []usage.Entry{}
	for rows.Next() {
		var raw []byte
		var entry usage.Entry
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &entry); err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, rows.Err()
}

func (s *MongoStore) BeginUsage(ctx context.Context, e usage.Entry) (bool, error) {
	if err := checkUsage(e); err != nil {
		return false, err
	}
	body, err := jsonDocument(e)
	if err != nil {
		return false, err
	}
	_, err = s.usage.InsertOne(ctx, bson.M{"_id": e.ID, "ancestors": e.Ancestors, "status": "pending", "body": body})
	if mongo.IsDuplicateKeyError(err) {
		return false, nil
	}
	return err == nil, err
}
func (s *MongoStore) FinishUsage(ctx context.Context, e usage.Entry) error {
	body, err := jsonDocument(e)
	if err != nil {
		return err
	}
	result, err := s.usage.UpdateOne(ctx, bson.M{"_id": e.ID, "status": "pending"}, bson.M{"$set": bson.M{"body": body, "status": e.Status}})
	if err != nil {
		return err
	}
	if result.MatchedCount > 0 {
		return nil
	}
	if err := s.usage.FindOne(ctx, bson.M{"_id": e.ID}).Err(); errors.Is(err, mongo.ErrNoDocuments) {
		return ErrNotFound
	} else {
		return err
	}
}
func (s *MongoStore) QueryUsage(ctx context.Context, f usage.Filter) ([]usage.Entry, error) {
	filter := bson.M{"ancestors": f.RunKey, "_id": bson.M{"$gt": f.After}}
	if f.All {
		delete(filter, "ancestors")
	}
	if f.StartedFrom != "" || f.StartedBefore != "" {
		bounds := bson.M{}
		if f.StartedFrom != "" {
			bounds["$gte"] = f.StartedFrom
		}
		if f.StartedBefore != "" {
			bounds["$lt"] = f.StartedBefore
		}
		filter["body.started_at"] = bounds
	}
	if f.Model != "" {
		filter["body.model"] = f.Model
	}
	if f.StepID != "" {
		filter["body.step_id"] = f.StepID
	}
	cursor, err := s.usage.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(usageLimit(f))))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	result := []usage.Entry{}
	for cursor.Next(ctx) {
		var doc struct {
			Body bson.M `bson:"body"`
		}
		if err = cursor.Decode(&doc); err != nil {
			return nil, err
		}
		var entry usage.Entry
		if err = fromJSONValue(doc.Body, &entry); err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, cursor.Err()
}
