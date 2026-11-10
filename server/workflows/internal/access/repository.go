package access

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/example/runloom/workflows/internal/recordset"

	_ "github.com/jackc/pgx/v5/stdlib"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Repository uses compare-and-swap so membership changes are atomic across API replicas.
type Repository interface {
	Module(string) Repository
	ReadOwned(context.Context) (State, int64, error)
	Read(context.Context) (State, int64, error)
	Swap(context.Context, int64, State) (bool, error)
	Close(context.Context) error
}

type document struct {
	ID       string `bson:"_id" json:"id"`
	Revision int64  `bson:"revision" json:"revision"`
	Body     string `bson:"body" json:"body"`
}
type repository struct {
	scope      string
	entities   recordset.Repository
	db         *sql.DB
	mongo      *mongo.Client
	collection *mongo.Collection
	file       string
	mu         *sync.Mutex
}

func Open(ctx context.Context, driver, url, database, dir string) (Repository, error) {
	r := &repository{scope: "identity", mu: &sync.Mutex{}}
	initial, _ := json.Marshal(NewState())
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
		r.entities = recordset.Postgres(db, "identity")
	case "mongodb", "mongo":
		c, err := mongo.Connect(options.Client().ApplyURI(url).SetMaxPoolSize(4))
		if err != nil {
			return nil, err
		}
		r.mongo = c
		r.collection = c.Database(database).Collection("runloom_identity_entities")
		for _, scope := range []string{"identity", "secrets", "consumers"} {
			collection := c.Database(database).Collection("runloom_" + scope + "_entities")
			_, err = collection.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "scope", Value: 1}, {Key: "key", Value: 1}}})
			if err != nil {
				c.Disconnect(ctx)
				return nil, err
			}
		}
		r.entities = recordset.Mongo(c, r.collection, "identity")
	case "", "file":
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
		r.file = filepath.Join(dir, "_identity.json")
		if _, err := os.Stat(r.file); errors.Is(err, os.ErrNotExist) {
			if err = r.writeFile(document{ID: "directory", Body: string(initial)}); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported identity storage driver")
	}
	return r, nil
}
func (r *repository) Module(scope string) Repository {
	if scope != "identity" && scope != "secrets" && scope != "consumers" {
		panic("unknown account module")
	}
	copy := *r
	copy.scope = scope
	if r.db != nil {
		copy.entities = recordset.Postgres(r.db, scope)
	}
	if r.mongo != nil {
		copy.entities = recordset.Mongo(r.mongo, r.collection.Database().Collection("runloom_"+scope+"_entities"), scope)
	}
	if r.file != "" {
		copy.file = filepath.Join(filepath.Dir(r.file), "_"+scope+".json")
	}
	return &copy
}

func (r *repository) Read(ctx context.Context) (State, int64, error) {
	state, revision, err := r.ReadOwned(ctx)
	if err != nil || r.scope != "identity" {
		return state, revision, err
	}
	for _, scope := range []string{"secrets", "consumers"} {
		part, _, err := r.Module(scope).Read(ctx)
		if err != nil {
			return state, revision, err
		}
		if scope == "secrets" {
			state.Connections = part.Connections
			state.Secrets = part.Secrets
		} else {
			state.ServiceAccounts = part.ServiceAccounts
		}
		state.Events = append(state.Events, part.Events...)
	}
	sort.SliceStable(state.Events, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339Nano, state.Events[i].CreatedAt)
		b, _ := time.Parse(time.RFC3339Nano, state.Events[j].CreatedAt)
		return a.Before(b)
	})
	return state, revision, nil
}

func (r *repository) ReadOwned(ctx context.Context) (State, int64, error) {
	if r.entities != nil {
		records, revision, err := r.entities.Read(ctx)
		state := NewState()
		if err == nil {
			err = recordset.Decode(records, &state)
		}
		return state, revision, err
	}
	var d document
	r.mu.Lock()
	defer r.mu.Unlock()
	raw, err := os.ReadFile(r.file)
	if errors.Is(err, os.ErrNotExist) {
		return NewState(), 0, nil
	}
	if err == nil {
		err = json.Unmarshal(raw, &d)
	}
	s := NewState()
	if err == nil {
		err = json.Unmarshal([]byte(d.Body), &s)
	}
	return s, d.Revision, err
}
func (r *repository) Swap(ctx context.Context, revision int64, s State) (bool, error) {
	if r.entities != nil {
		records, err := recordset.Encode(s)
		if err != nil {
			return false, err
		}
		return r.entities.Swap(ctx, revision, records)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	data, err := os.ReadFile(r.file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	var previous document
	if len(data) > 0 {
		if err = json.Unmarshal(data, &previous); err != nil {
			return false, err
		}
	}
	if previous.Revision != revision {
		return false, nil
	}
	err = r.writeFile(document{ID: "directory", Revision: revision + 1, Body: string(raw)})
	return err == nil, err
}
func (r *repository) writeFile(d document) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(r.file), ".access-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), r.file)
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
