package chat

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"time"
)

var ErrNotFound = errors.New("conversation not found")
var ErrConflict = errors.New("conversation changed; reload and retry")

type Store struct {
	pg         *pgxpool.Pool
	mongo      *mongo.Client
	collection *mongo.Collection
}
type record struct {
	ID       string `bson:"_id"`
	User     string `bson:"user"`
	Org      string `bson:"org"`
	Team     string `bson:"team"`
	Workflow string `bson:"workflow"`
	Revision int64  `bson:"revision"`
	Active   bool   `bson:"active"`
	Data     []byte `bson:"data"`
}

func Open(ctx context.Context, driver, uri, name string) (*Store, error) {
	s := &Store{}
	var err error
	switch driver {
	case "postgres":
		s.pg, err = pgxpool.New(ctx, uri)
		if err != nil {
			return nil, err
		}
		_, err = s.pg.Exec(ctx, `CREATE TABLE IF NOT EXISTS assistant_conversations (id text PRIMARY KEY, owner_id text NOT NULL, org_id text NOT NULL, team_id text NOT NULL, revision bigint NOT NULL, active boolean NOT NULL, data bytea NOT NULL); CREATE INDEX IF NOT EXISTS assistant_scope ON assistant_conversations(owner_id,org_id,team_id,id); ALTER TABLE assistant_conversations ADD COLUMN IF NOT EXISTS workflow_id text NOT NULL DEFAULT ''; CREATE INDEX IF NOT EXISTS assistant_workflow_scope ON assistant_conversations(owner_id,org_id,team_id,workflow_id,id); CREATE INDEX IF NOT EXISTS assistant_pending ON assistant_conversations(active) WHERE active`)
	case "mongodb":
		s.mongo, err = mongo.Connect(options.Client().ApplyURI(uri))
		if err != nil {
			return nil, err
		}
		s.collection = s.mongo.Database(name).Collection("assistant_conversations")
		_, err = s.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "user", Value: 1}, {Key: "org", Value: 1}, {Key: "team", Value: 1}, {Key: "workflow", Value: 1}, {Key: "_id", Value: 1}}}, {Keys: bson.D{{Key: "active", Value: 1}}}})
	default:
		return nil, errors.New("assistant requires postgres or mongodb")
	}
	if err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if s.pg != nil {
		s.pg.Close()
	}
	if s.mongo != nil {
		_ = s.mongo.Disconnect(ctx)
	}
}
func (s *Store) Ping(ctx context.Context) error {
	if s.pg != nil {
		return s.pg.Ping(ctx)
	}
	return s.mongo.Ping(ctx, nil)
}
func pack(c Conversation) record {
	b, _ := json.Marshal(c)
	return record{c.ID, c.Scope.User, c.Scope.Org, c.Scope.Team, c.Scope.Workflow, c.Revision, c.Active(), b}
}
func unpack(r record) (Conversation, error) {
	var c Conversation
	err := json.Unmarshal(r.Data, &c)
	c.Scope.User = r.User
	return c, err
}
func (s *Store) Create(ctx context.Context, c Conversation) error {
	r := pack(c)
	if s.pg != nil {
		_, err := s.pg.Exec(ctx, "INSERT INTO assistant_conversations(id,owner_id,org_id,team_id,workflow_id,revision,active,data) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", r.ID, r.User, r.Org, r.Team, r.Workflow, r.Revision, r.Active, r.Data)
		return err
	}
	_, err := s.collection.InsertOne(ctx, r)
	return err
}
func (s *Store) Get(ctx context.Context, id string) (Conversation, error) {
	var r record
	if s.pg != nil {
		err := s.pg.QueryRow(ctx, "SELECT id,owner_id,org_id,team_id,workflow_id,revision,active,data FROM assistant_conversations WHERE id=$1", id).Scan(&r.ID, &r.User, &r.Org, &r.Team, &r.Workflow, &r.Revision, &r.Active, &r.Data)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return Conversation{}, ErrNotFound
			}
			return Conversation{}, err
		}
	} else {
		if err := s.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&r); err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				return Conversation{}, ErrNotFound
			}
			return Conversation{}, err
		}
	}
	return unpack(r)
}
func (s *Store) Save(ctx context.Context, c *Conversation) error {
	old := c.Revision
	next := *c
	next.Revision++
	next.UpdatedAt = time.Now().UTC()
	r := pack(next)
	if s.pg != nil {
		result, err := s.pg.Exec(ctx, "UPDATE assistant_conversations SET revision=$1,active=$2,data=$3 WHERE id=$4 AND revision=$5", r.Revision, r.Active, r.Data, r.ID, old)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return ErrConflict
		}
		*c = next
		return nil
	}
	result, err := s.collection.ReplaceOne(ctx, bson.M{"_id": r.ID, "revision": old}, r)
	if err != nil {
		return err
	}
	if result.MatchedCount != 1 {
		return ErrConflict
	}
	*c = next
	return nil
}

// List uses an ID cursor rather than offsets. Pending scans are bounded too.
func (s *Store) List(ctx context.Context, scope *Scope, after string, limit int) ([]Conversation, error) {
	records := []record{}
	if s.pg != nil {
		query := "SELECT id,owner_id,org_id,team_id,workflow_id,revision,active,data FROM assistant_conversations WHERE active=true ORDER BY id LIMIT $1"
		args := []any{limit}
		if scope != nil {
			query = "SELECT id,owner_id,org_id,team_id,workflow_id,revision,active,data FROM assistant_conversations WHERE owner_id=$1 AND org_id=$2 AND team_id=$3 AND workflow_id=$4 AND id>$5 ORDER BY id LIMIT $6"
			args = []any{scope.User, scope.Org, scope.Team, scope.Workflow, after, limit}
		}
		rows, err := s.pg.Query(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var r record
			if err = rows.Scan(&r.ID, &r.User, &r.Org, &r.Team, &r.Workflow, &r.Revision, &r.Active, &r.Data); err != nil {
				return nil, err
			}
			records = append(records, r)
		}
		if err = rows.Err(); err != nil {
			return nil, err
		}
	} else {
		filter := bson.M{"active": true}
		if scope != nil {
			filter = bson.M{"user": scope.User, "org": scope.Org, "team": scope.Team, "workflow": scope.Workflow, "_id": bson.M{"$gt": after}}
			// Existing general chats predate the workflow metadata field.
			if scope.Workflow == "" {
				delete(filter, "workflow")
				filter["$or"] = bson.A{bson.M{"workflow": ""}, bson.M{"workflow": bson.M{"$exists": false}}}
			}
		}
		cur, err := s.collection.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(limit)))
		if err != nil {
			return nil, err
		}
		defer cur.Close(ctx)
		if err = cur.All(ctx, &records); err != nil {
			return nil, err
		}
	}
	out := []Conversation{}
	for _, r := range records {
		c, err := unpack(r)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}
