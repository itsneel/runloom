// Package recordset persists normalized entity records with atomic aggregate
// updates. A scope is owned by one module; it is never a cross-service transaction.
package recordset

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

type Records map[string]json.RawMessage
type Repository interface {
	Read(context.Context) (Records, int64, error)
	Swap(context.Context, int64, Records) (bool, error)
}

// Encode stores each map member and array element as a separate record. No
// unbounded audit/history array is stored inside an entity document.
func Encode(value any) (Records, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	result := Records{}
	for name, raw := range fields {
		switch {
		case len(raw) > 0 && raw[0] == '{':
			var values map[string]json.RawMessage
			if err := json.Unmarshal(raw, &values); err != nil {
				return nil, err
			}
			result[name+"/type"] = json.RawMessage(`"map"`)
			for key, v := range values {
				result[name+"/map/"+key] = v
			}
		case len(raw) > 0 && raw[0] == '[':
			var values []json.RawMessage
			if err := json.Unmarshal(raw, &values); err != nil {
				return nil, err
			}
			result[name+"/type"] = json.RawMessage(`"array"`)
			for i, v := range values {
				result[fmt.Sprintf("%s/array/%020d", name, i)] = v
			}
		default:
			result[name+"/value"] = raw
		}
	}
	return result, nil
}

func Decode(records Records, value any) error {
	fields := map[string]any{}
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts := strings.SplitN(key, "/", 3)
		if len(parts) < 2 {
			return fmt.Errorf("invalid entity key")
		}
		name := parts[0]
		switch parts[1] {
		case "type":
			if fields[name] != nil {
				continue
			}
			if string(records[key]) == `"map"` {
				fields[name] = map[string]json.RawMessage{}
			} else {
				fields[name] = []json.RawMessage{}
			}
		case "map":
			if len(parts) != 3 {
				return fmt.Errorf("invalid map key")
			}
			if fields[name] == nil {
				fields[name] = map[string]json.RawMessage{}
			}
			fields[name].(map[string]json.RawMessage)[parts[2]] = records[key]
		case "array":
			if len(parts) != 3 {
				return fmt.Errorf("invalid array key")
			}
			if fields[name] == nil {
				fields[name] = []json.RawMessage{}
			}
			values := fields[name].([]json.RawMessage)
			index, err := strconv.Atoi(parts[2])
			if err != nil || index != len(values) {
				return fmt.Errorf("invalid entity sequence")
			}
			fields[name] = append(values, records[key])
		case "value":
			fields[name] = records[key]
		default:
			return fmt.Errorf("invalid entity kind")
		}
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, value)
}

type postgres struct {
	db    *sql.DB
	scope string
}

func Postgres(db *sql.DB, scope string) Repository { return &postgres{db, scope} }

const Schema = `CREATE TABLE IF NOT EXISTS runloom_entity_heads(scope TEXT PRIMARY KEY, revision BIGINT NOT NULL);
CREATE TABLE IF NOT EXISTS runloom_entity_records(scope TEXT NOT NULL, key TEXT NOT NULL, body JSONB NOT NULL, PRIMARY KEY(scope,key));`

func (p *postgres) Read(ctx context.Context) (Records, int64, error) {
	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM runloom_entity_heads WHERE scope=$1`, p.scope).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return Records{}, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT key,body FROM runloom_entity_records WHERE scope=$1`, p.scope)
	if err != nil {
		return nil, 0, err
	}
	result := Records{}
	for rows.Next() {
		var key string
		var raw []byte
		if err = rows.Scan(&key, &raw); err != nil {
			rows.Close()
			return nil, 0, err
		}
		result[key] = raw
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	return result, revision, tx.Commit()
}

func (p *postgres) Swap(ctx context.Context, revision int64, records Records) (bool, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO runloom_entity_heads(scope,revision) VALUES($1,0) ON CONFLICT DO NOTHING`, p.scope)
	if err != nil {
		return false, err
	}
	r, err := tx.ExecContext(ctx, `UPDATE runloom_entity_heads SET revision=revision+1 WHERE scope=$1 AND revision=$2`, p.scope, revision)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT key,body FROM runloom_entity_records WHERE scope=$1`, p.scope)
	if err != nil {
		return false, err
	}
	previous := Records{}
	for rows.Next() {
		var key string
		var raw []byte
		if err = rows.Scan(&key, &raw); err != nil {
			rows.Close()
			return false, err
		}
		previous[key] = raw
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	for key, raw := range records {
		if equal(previous[key], raw) {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO runloom_entity_records(scope,key,body) VALUES($1,$2,$3) ON CONFLICT(scope,key) DO UPDATE SET body=EXCLUDED.body`, p.scope, key, []byte(raw)); err != nil {
			return false, err
		}
	}
	for key := range previous {
		if _, ok := records[key]; !ok {
			if _, err = tx.ExecContext(ctx, `DELETE FROM runloom_entity_records WHERE scope=$1 AND key=$2`, p.scope, key); err != nil {
				return false, err
			}
		}
	}
	err = tx.Commit()
	return err == nil, err
}

func equal(a, b []byte) bool {
	// PostgreSQL JSONB canonicalizes object key order; compare JSON values,
	// not serialization bytes, so unrelated entities are not rewritten.
	var x, y any
	da, db := json.NewDecoder(bytes.NewReader(a)), json.NewDecoder(bytes.NewReader(b))
	da.UseNumber()
	db.UseNumber()
	if da.Decode(&x) != nil || db.Decode(&y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

type mongoRepository struct {
	client     *mongo.Client
	collection *mongo.Collection
	scope      string
}

func Mongo(client *mongo.Client, collection *mongo.Collection, scope string) Repository {
	return &mongoRepository{client, collection, scope}
}

type document struct {
	ID       string `bson:"_id"`
	Scope    string `bson:"scope"`
	Key      string `bson:"key"`
	Revision int64  `bson:"revision"`
	Body     string `bson:"body"`
}

func (m *mongoRepository) transaction(ctx context.Context, fn func(context.Context) (any, error)) (any, error) {
	session, err := m.client.StartSession()
	if err != nil {
		return nil, err
	}
	defer session.EndSession(ctx)
	return session.WithTransaction(ctx, fn, options.Transaction().SetReadConcern(readconcern.Snapshot()).SetWriteConcern(writeconcern.Majority()))
}
func (m *mongoRepository) read(ctx context.Context) (Records, int64, error) {
	var head document
	err := m.collection.FindOne(ctx, bson.M{"_id": m.scope + "/head"}).Decode(&head)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Records{}, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	cursor, err := m.collection.Find(ctx, bson.M{"scope": m.scope, "key": bson.M{"$ne": ""}})
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)
	result := Records{}
	for cursor.Next(ctx) {
		var d document
		if err := cursor.Decode(&d); err != nil {
			return nil, 0, err
		}
		result[d.Key] = json.RawMessage(d.Body)
	}
	return result, head.Revision, cursor.Err()
}
func (m *mongoRepository) Read(ctx context.Context) (Records, int64, error) {
	var records Records
	var revision int64
	_, err := m.transaction(ctx, func(ctx context.Context) (any, error) {
		var err error
		records, revision, err = m.read(ctx)
		return nil, err
	})
	return records, revision, err
}

var conflict = errors.New("record revision conflict")

func (m *mongoRepository) Swap(ctx context.Context, revision int64, records Records) (bool, error) {
	// Head creation is independently idempotent; all entity changes commit with
	// the revision in the transaction. MongoDB must be a replica set.
	_, err := m.collection.UpdateOne(ctx, bson.M{"_id": m.scope + "/head"}, bson.M{"$setOnInsert": document{ID: m.scope + "/head", Scope: m.scope}}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return false, err
	}
	_, err = m.transaction(ctx, func(ctx context.Context) (any, error) {
		r, err := m.collection.UpdateOne(ctx, bson.M{"_id": m.scope + "/head", "revision": revision}, bson.M{"$inc": bson.M{"revision": 1}})
		if err != nil {
			return nil, err
		}
		if r.MatchedCount != 1 {
			return nil, conflict
		}
		previous, _, err := m.read(ctx)
		if err != nil {
			return nil, err
		}
		for key, raw := range records {
			if equal(previous[key], raw) {
				continue
			}
			id := m.scope + "/record/" + key
			_, err = m.collection.ReplaceOne(ctx, bson.M{"_id": id}, document{ID: id, Scope: m.scope, Key: key, Body: string(raw)}, options.Replace().SetUpsert(true))
			if err != nil {
				return nil, err
			}
		}
		for key := range previous {
			if _, ok := records[key]; !ok {
				if _, err = m.collection.DeleteOne(ctx, bson.M{"_id": m.scope + "/record/" + key}); err != nil {
					return nil, err
				}
			}
		}
		return nil, nil
	})
	if errors.Is(err, conflict) {
		return false, nil
	}
	return err == nil, err
}
