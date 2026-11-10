// Package consumer owns durable consumer admission records, not identity credentials.
package consumer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/example/runloom/workflows/internal/protection"
	"github.com/example/runloom/workflows/internal/recordset"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var ErrNotFound = errors.New("consumer document not found")
var fileMu sync.Mutex // filesystem driver supports one API process only
var keyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,200}$`)

type Repository interface {
	Read(context.Context, string, any) (int64, error)
	Swap(context.Context, string, int64, any) (bool, error)
	Close(context.Context) error
}
type document struct {
	ID       string `bson:"_id" json:"id"`
	Revision int64  `bson:"revision" json:"revision"`
	Body     string `bson:"body" json:"body"`
}
type repository struct {
	db         *sql.DB
	mongo      *mongo.Client
	collection *mongo.Collection
	dir        string
}

func Open(ctx context.Context, driver, url, database, dir string) (Repository, error) {
	r := &repository{}
	switch driver {
	case "postgres", "postgresql":
		db, err := sql.Open("pgx", url)
		if err != nil {
			return nil, err
		}
		r.db = db
		db.SetMaxOpenConns(4)
		if _, err = db.ExecContext(ctx, recordset.Schema); err != nil {
			db.Close()
			return nil, err
		}
	case "mongo", "mongodb":
		c, err := mongo.Connect(options.Client().ApplyURI(url).SetMaxPoolSize(4))
		if err != nil {
			return nil, err
		}
		r.mongo = c
		r.collection = c.Database(database).Collection("runloom_admission_entities")
		if err = c.Ping(ctx, nil); err != nil {
			c.Disconnect(ctx)
			return nil, err
		}
		if _, err = r.collection.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "scope", Value: 1}, {Key: "key", Value: 1}}}); err != nil {
			c.Disconnect(ctx)
			return nil, err
		}
	case "", "file":
		r.dir = filepath.Join(dir, "_consumers")
		if err := os.MkdirAll(r.dir, 0700); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported consumer storage")
	}
	return r, nil
}
func (r *repository) read(ctx context.Context, key string) (document, error) {
	var d document
	var err error
	if !keyPattern.MatchString(key) {
		return d, fmt.Errorf("invalid document key")
	}
	var raw []byte
	raw, err = os.ReadFile(filepath.Join(r.dir, key+".json"))
	if err == nil {
		err = json.Unmarshal(raw, &d)
	}
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, mongo.ErrNoDocuments) || errors.Is(err, os.ErrNotExist) {
		err = ErrNotFound
	}
	return d, err
}
func (r *repository) Read(ctx context.Context, key string, v any) (int64, error) {
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	if !keyPattern.MatchString(key) {
		return -1, fmt.Errorf("invalid document key")
	}
	if entities := r.entities(key); entities != nil {
		records, revision, err := entities.Read(ctx)
		if err != nil {
			return -1, err
		}
		if revision == 0 {
			return -1, ErrNotFound
		}
		for id, raw := range records {
			var value json.RawMessage
			if err := protection.Unmarshal("admission/"+key+"/"+id, raw, &value); err != nil {
				return -1, err
			}
			records[id] = value
		}
		return revision - 1, recordset.Decode(records, v)
	}
	if r.dir != "" {
		fileMu.Lock()
		defer fileMu.Unlock()
	}
	d, err := r.read(ctx, key)
	if err != nil {
		return -1, err
	}
	return d.Revision, protection.Unmarshal("admission/"+key, []byte(d.Body), v)
}

// A revision of -1 means create if absent. CAS serializes admissions across replicas.
func (r *repository) Swap(ctx context.Context, key string, revision int64, v any) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !keyPattern.MatchString(key) {
		return false, fmt.Errorf("invalid document key")
	}
	if entities := r.entities(key); entities != nil {
		records, err := recordset.Encode(v)
		if err != nil {
			return false, err
		}
		for id, raw := range records {
			encoded, err := protection.Marshal("admission/"+key+"/"+id, raw)
			if err != nil {
				return false, err
			}
			records[id] = encoded
		}
		return entities.Swap(ctx, revision+1, records)
	}
	raw, err := protection.Marshal("admission/"+key, v)
	if err != nil {
		return false, err
	}
	if len(raw) > 8*1024*1024 {
		return false, fmt.Errorf("consumer storage capacity reached")
	}
	d := document{ID: key, Revision: revision + 1, Body: string(raw)}
	fileMu.Lock()
	defer fileMu.Unlock()
	old, err := r.read(ctx, key)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return false, err
	}
	if (errors.Is(err, ErrNotFound) && revision != -1) || (err == nil && old.Revision != revision) {
		return false, nil
	}
	encoded, _ := json.Marshal(d)
	f, err := os.CreateTemp(r.dir, ".consumer-")
	if err != nil {
		return false, err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(encoded); err != nil {
		f.Close()
		return false, err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return false, err
	}
	if err = f.Close(); err != nil {
		return false, err
	}
	if err = os.Rename(f.Name(), filepath.Join(r.dir, key+".json")); err != nil {
		return false, err
	}
	directory, err := os.Open(r.dir)
	if err != nil {
		return false, err
	}
	defer directory.Close()
	err = directory.Sync()
	return err == nil, err
}
func (r *repository) Close(ctx context.Context) error {
	if r.db != nil {
		return r.db.Close()
	}
	if r.mongo != nil {
		return r.mongo.Disconnect(ctx)
	}
	return nil
}

func (r *repository) entities(key string) recordset.Repository {
	if r.db != nil {
		return recordset.Postgres(r.db, "admission/"+key)
	}
	if r.mongo != nil {
		return recordset.Mongo(r.mongo, r.collection, key)
	}
	return nil
}
