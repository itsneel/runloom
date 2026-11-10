package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/example/runloom/workflows/internal/authoring"
	"github.com/example/runloom/workflows/internal/model"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var ErrAuthoringConflict = errors.New("authoring revision or request conflicts; refresh and create a new proposal")

type AuthoringReceipt struct {
	RequestID   string `json:"request_id"`
	Fingerprint string `json:"-"`
	ParentID    string `json:"parent_id"`
	ChildID     string `json:"child_id"`
	DraftOnly   bool   `json:"draft_only"`
	ParentHash  string `json:"parent_hash,omitempty"`
}

// The persisted receipt includes its private request digest, which is never sent
// to the browser. The receipt and all affected drafts commit together.
type authoringRecord struct {
	Receipt     AuthoringReceipt `json:"receipt"`
	Fingerprint string           `json:"fingerprint"`
}
type AuthoringBatch struct {
	Restore                                              bool
	RequestID, Fingerprint, Actor, ParentHash, ProjectID string
	Mode                                                 string
	SourceVersion                                        int
	Parent, Child                                        model.Definition
}
type Authoring interface {
	AuthoringReceipt(context.Context, string) (AuthoringReceipt, error)
	CommitAuthoring(context.Context, AuthoringBatch) (AuthoringReceipt, error)
}

func (b AuthoringBatch) definitions() []model.Definition {
	if b.Child.ID == "" {
		return []model.Definition{b.Parent}
	}
	return []model.Definition{b.Child, b.Parent}
}
func (b AuthoringBatch) assignmentID() string {
	if b.Mode == "create" {
		return b.Parent.ID
	}
	return b.Child.ID
}
func (b AuthoringBatch) message() string {
	if b.Restore {
		return "Assistant: restore previous draft"
	}
	if b.Mode == "create" {
		return "Assistant: create workflow draft"
	}
	if b.Mode == "update" {
		return "Assistant: update workflow draft"
	}
	return "Assistant: create and attach child draft"
}
func receipt(b AuthoringBatch) AuthoringReceipt {
	return AuthoringReceipt{RequestID: b.RequestID, Fingerprint: b.Fingerprint, ParentID: b.Parent.ID, ChildID: b.Child.ID, DraftOnly: true, ParentHash: authoring.Fingerprint(b.Parent)}
}
func auditAuthoring(b AuthoringBatch) AuditEvent {
	event := "definition.assistant_child_attached"
	if b.Mode == "create" {
		event = "definition.assistant_created"
	} else if b.Mode == "update" {
		event = "definition.assistant_updated"
	}
	if b.Restore {
		event = "definition.assistant_restored"
	}
	return AuditEvent{DefinitionID: b.Parent.ID, EventType: event, Actor: b.Actor, Payload: map[string]any{"request_id": b.RequestID, "child_id": b.Child.ID, "draft_only": true}}
}
func decodeReceipt(raw []byte) (AuthoringReceipt, error) {
	var value authoringRecord
	err := json.Unmarshal(raw, &value)
	value.Receipt.Fingerprint = value.Fingerprint
	return value.Receipt, err
}
func encodeReceipt(r AuthoringReceipt) []byte {
	b, _ := json.Marshal(authoringRecord{r, r.Fingerprint})
	return b
}

func (s *PostgresStore) AuthoringReceipt(ctx context.Context, id string) (AuthoringReceipt, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT body FROM runloom_entity_records WHERE scope='authoring' AND key=$1`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return AuthoringReceipt{}, ErrNotFound
	}
	if err != nil {
		return AuthoringReceipt{}, err
	}
	return decodeReceipt(raw)
}
func (s *PostgresStore) CommitAuthoring(ctx context.Context, b AuthoringBatch) (AuthoringReceipt, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AuthoringReceipt{}, err
	}
	defer tx.Rollback()
	// Every authoring request takes the request lock before the root lock.
	for _, key := range []string{"authoring:" + b.RequestID, b.Parent.ID, b.Child.ID} {
		if key == "" {
			continue
		}
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, key); err != nil {
			return AuthoringReceipt{}, err
		}
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT body FROM runloom_entity_records WHERE scope='authoring' AND key=$1`, b.RequestID).Scan(&raw)
	if err == nil {
		r, e := decodeReceipt(raw)
		if e == nil && r.Fingerprint != b.Fingerprint {
			e = ErrAuthoringConflict
		}
		return r, e
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return AuthoringReceipt{}, err
	}
	if b.Mode == "create" {
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runloom_definitions WHERE definition_id=$1)`, b.Parent.ID).Scan(&exists); err != nil {
			return AuthoringReceipt{}, err
		}
		if exists {
			return AuthoringReceipt{}, ErrAuthoringConflict
		}
	} else {
		err = tx.QueryRowContext(ctx, `SELECT body FROM runloom_definitions WHERE definition_id=$1 AND version=$2 FOR UPDATE`, b.Parent.ID, b.SourceVersion).Scan(&raw)
		if err != nil {
			return AuthoringReceipt{}, err
		}
		var parent model.Definition
		if err = json.Unmarshal(raw, &parent); err != nil {
			return AuthoringReceipt{}, err
		}
		if authoring.Fingerprint(parent) != b.ParentHash {
			return AuthoringReceipt{}, ErrAuthoringConflict
		}
		if b.Child.ID != "" {
			var exists bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runloom_definitions WHERE definition_id=$1)`, b.Child.ID).Scan(&exists); err != nil {
				return AuthoringReceipt{}, err
			}
			if exists {
				return AuthoringReceipt{}, ErrAuthoringConflict
			}
		}
	}
	for _, d := range b.definitions() {
		body, e := json.Marshal(d)
		if e != nil {
			return AuthoringReceipt{}, e
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO runloom_definitions(definition_id,version,body) VALUES($1,0,$2) ON CONFLICT(definition_id,version) DO UPDATE SET body=EXCLUDED.body,created_at=CURRENT_TIMESTAMP`, d.ID, body); err != nil {
			return AuthoringReceipt{}, err
		}
		if _, err = insertPostgresCommit(tx, d, CommitMeta{Actor: b.Actor, Message: b.message()}, "save", nil, ""); err != nil {
			return AuthoringReceipt{}, err
		}
	}
	if b.assignmentID() != "" {
		// Participate in the metadata CAS protocol so concurrent project changes
		// cannot silently remove the new child's assignment.
		if _, err = tx.ExecContext(ctx, `INSERT INTO runloom_entity_heads(scope,revision) VALUES('projects',1) ON CONFLICT(scope) DO UPDATE SET revision=runloom_entity_heads.revision+1`); err != nil {
			return AuthoringReceipt{}, err
		}
		if b.ProjectID != DefaultProjectID {
			var exists bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runloom_entity_records WHERE scope='projects' AND key=$1)`, "projects/map/"+b.ProjectID).Scan(&exists); err != nil {
				return AuthoringReceipt{}, err
			}
			if !exists {
				return AuthoringReceipt{}, ErrProjectNotFound
			}
		}
		project, _ := json.Marshal(b.ProjectID)
		if _, err = tx.ExecContext(ctx, `INSERT INTO runloom_entity_records(scope,key,body) VALUES('projects',$1,$2) ON CONFLICT(scope,key) DO UPDATE SET body=EXCLUDED.body`, "workflow_projects/map/"+b.assignmentID(), project); err != nil {
			return AuthoringReceipt{}, err
		}
	}
	if _, err = appendPostgresAudit(tx, auditAuthoring(b)); err != nil {
		return AuthoringReceipt{}, err
	}
	result := receipt(b)
	if _, err = tx.ExecContext(ctx, `INSERT INTO runloom_entity_records(scope,key,body) VALUES('authoring',$1,$2)`, b.RequestID, encodeReceipt(result)); err != nil {
		return AuthoringReceipt{}, err
	}
	return result, tx.Commit()
}

func (s *MongoStore) AuthoringReceipt(ctx context.Context, id string) (AuthoringReceipt, error) {
	return s.authoringReceipt(ctx, id)
}
func (s *MongoStore) authoringReceipt(ctx context.Context, id string) (AuthoringReceipt, error) {
	var doc struct {
		Body string `bson:"body"`
	}
	err := s.projects.FindOne(ctx, bson.M{"_id": "authoring/record/" + id}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AuthoringReceipt{}, ErrNotFound
	}
	if err != nil {
		return AuthoringReceipt{}, err
	}
	return decodeReceipt([]byte(doc.Body))
}
func (s *MongoStore) CommitAuthoring(ctx context.Context, b AuthoringBatch) (AuthoringReceipt, error) {
	session, err := s.client.StartSession()
	if err != nil {
		return AuthoringReceipt{}, err
	}
	defer session.EndSession(ctx)
	result := receipt(b)
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		old, e := s.authoringReceipt(tx, b.RequestID)
		if e == nil {
			if old.Fingerprint != b.Fingerprint {
				return nil, ErrAuthoringConflict
			}
			result = old
			return nil, nil
		}
		if !errors.Is(e, ErrNotFound) {
			return nil, e
		}
		if b.Mode == "create" {
			if e = s.definitions.FindOne(tx, bson.M{"definition_id": b.Parent.ID}).Err(); e == nil {
				return nil, ErrAuthoringConflict
			} else if !errors.Is(e, mongo.ErrNoDocuments) {
				return nil, e
			}
		} else {
			var doc bson.M
			if e = s.definitions.FindOne(tx, bson.M{"definition_id": b.Parent.ID, "version": b.SourceVersion}).Decode(&doc); e != nil {
				return nil, e
			}
			var parent model.Definition
			if e = fromJSONValue(doc["body"], &parent); e != nil {
				return nil, e
			}
			if authoring.Fingerprint(parent) != b.ParentHash {
				return nil, ErrAuthoringConflict
			}
			if b.Child.ID != "" {
				if e = s.definitions.FindOne(tx, bson.M{"definition_id": b.Child.ID}).Err(); e == nil {
					return nil, ErrAuthoringConflict
				} else if !errors.Is(e, mongo.ErrNoDocuments) {
					return nil, e
				}
			}
		}
		for _, d := range b.definitions() {
			body, e := jsonDocument(d)
			if e != nil {
				return nil, e
			}
			if _, e = s.insertCommit(tx, d, CommitMeta{Actor: b.Actor, Message: b.message()}, "save", nil, ""); e != nil {
				return nil, e
			}
			if _, e = s.definitions.ReplaceOne(tx, bson.M{"definition_id": d.ID, "version": 0}, bson.M{"definition_id": d.ID, "version": 0, "body": body, "created_at": utcNow()}, options.Replace().SetUpsert(true)); e != nil {
				return nil, e
			}
		}
		if b.assignmentID() != "" {
			if _, e = s.projects.UpdateOne(tx, bson.M{"_id": "projects/head"}, bson.M{"$inc": bson.M{"revision": 1}, "$set": bson.M{"scope": "projects", "key": ""}}, options.UpdateOne().SetUpsert(true)); e != nil {
				return nil, e
			}
			if b.ProjectID != DefaultProjectID {
				if e = s.projects.FindOne(tx, bson.M{"_id": "projects/record/projects/map/" + b.ProjectID}).Err(); errors.Is(e, mongo.ErrNoDocuments) {
					return nil, ErrProjectNotFound
				} else if e != nil {
					return nil, e
				}
			}
			key := "workflow_projects/map/" + b.assignmentID()
			project, _ := json.Marshal(b.ProjectID)
			if _, e = s.projects.ReplaceOne(tx, bson.M{"_id": "projects/record/" + key}, bson.M{"_id": "projects/record/" + key, "scope": "projects", "key": key, "body": string(project)}, options.Replace().SetUpsert(true)); e != nil {
				return nil, e
			}
		}
		if _, e = s.appendAudit(tx, auditAuthoring(b)); e != nil {
			return nil, e
		}
		_, e = s.projects.InsertOne(tx, bson.M{"_id": "authoring/record/" + b.RequestID, "scope": "authoring", "key": b.RequestID, "body": string(encodeReceipt(result))})
		return nil, e
	})
	return result, err
}
