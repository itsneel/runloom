package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/example/runloom/workflows/internal/definition"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/recordset"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

type MongoStore struct {
	*promptStore
	*projectStore
	projects    *mongo.Collection
	client      *mongo.Client
	database    *mongo.Database
	definitions *mongo.Collection
	commits     *mongo.Collection
	counters    *mongo.Collection
	runs        *mongo.Collection
	audits      *mongo.Collection
	usage       *mongo.Collection
}

func NewMongo(ctx context.Context, url, databaseName string) (*MongoStore, error) {
	return NewMongoTeam(ctx, url, databaseName, "")
}

func NewMongoTeam(ctx context.Context, url, databaseName, prefix string) (*MongoStore, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(url).SetMaxPoolSize(4))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(context.Background())
		return nil, err
	}
	database := client.Database(databaseName)
	store := &MongoStore{
		projects:    database.Collection(prefix + "metadata_entities"),
		client:      client,
		database:    database,
		definitions: database.Collection(prefix + "definitions"),
		commits:     database.Collection(prefix + "definition_commits"),
		counters:    database.Collection(prefix + "definition_counters"),
		runs:        database.Collection(prefix + "workflow_runs"),
		audits:      database.Collection(prefix + "audit_events"),
		usage:       database.Collection(prefix + "llm_usage"),
	}
	_, err = store.projects.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "scope", Value: 1}, {Key: "key", Value: 1}}})
	if err != nil {
		client.Disconnect(context.Background())
		return nil, err
	}
	store.projectStore = newProjectStore(store.readProjects, store.swapProjects, store.Get)
	store.promptStore = &promptStore{repo: recordset.Mongo(store.client, store.projects, "prompts")}
	if err := store.createIndexes(ctx); err != nil {
		client.Disconnect(context.Background())
		return nil, err
	}
	return store, nil
}

func (s *MongoStore) createIndexes(ctx context.Context) error {
	if _, err := s.usage.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "ancestors", Value: 1}, {Key: "_id", Value: 1}}}); err != nil {
		return err
	}
	if _, err := s.usage.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "body.started_at", Value: 1}, {Key: "_id", Value: 1}}}); err != nil {
		return err
	}
	_, err := s.definitions.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "definition_id", Value: 1}, {Key: "version", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}
	_, err = s.commits.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "definition_id", Value: 1}, {Key: "sequence", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "commit_id", Value: 1}}, Options: options.Index().SetUnique(true)},
	})
	if err != nil {
		return err
	}
	_, err = s.runs.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "workflow_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "definition_id", Value: 1}, {Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "created_at", Value: -1}, {Key: "workflow_id", Value: -1}}},
		{Keys: bson.D{{Key: "metadata.project_id", Value: 1}, {Key: "created_at", Value: -1}, {Key: "workflow_id", Value: -1}}},
		{Keys: bson.D{{Key: "metadata.source", Value: 1}, {Key: "created_at", Value: -1}, {Key: "workflow_id", Value: -1}}},
		{Keys: bson.D{{Key: "metadata.root_execution_id", Value: 1}, {Key: "created_at", Value: -1}, {Key: "workflow_id", Value: -1}}},
	})
	if err != nil {
		return err
	}
	_, err = s.audits.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "event_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "workflow_id", Value: 1}, {Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "definition_id", Value: 1}, {Key: "created_at", Value: -1}}},
	})
	return err
}

func (s *MongoStore) Driver() string { return "mongodb" }

func (s *MongoStore) SaveDraft(value model.Definition) (model.Definition, error) {
	normalized, _, err := s.CommitDraft(value, CommitMeta{Actor: "system", Message: "Saved draft"})
	return normalized, err
}

func (s *MongoStore) CommitDraft(value model.Definition, meta CommitMeta) (model.Definition, DefinitionCommit, error) {
	normalized, err := definition.Draft(value)
	if err != nil {
		return normalized, DefinitionCommit{}, err
	}
	normalized.Version = 0
	body, err := jsonDocument(normalized)
	if err != nil {
		return normalized, DefinitionCommit{}, err
	}
	var commit DefinitionCommit
	err = s.transaction(func(ctx context.Context) error {
		var err error
		commit, err = s.insertCommit(ctx, normalized, meta, "save", nil, "")
		if err != nil {
			return err
		}
		_, err = s.definitions.ReplaceOne(ctx,
			bson.M{"definition_id": normalized.ID, "version": 0},
			bson.M{"definition_id": normalized.ID, "version": 0, "body": body, "created_at": utcNow()},
			options.Replace().SetUpsert(true))
		return err
	})
	return normalized, commit, err
}

func (s *MongoStore) Publish(id string) (model.Definition, error) {
	return s.PublishWithCommit(id, CommitMeta{Actor: "system"})
}

func (s *MongoStore) PublishWithCommit(id string, meta CommitMeta) (model.Definition, error) {
	var value model.Definition
	err := s.transaction(func(ctx context.Context) error {
		var err error
		value, err = s.publishWithCommit(ctx, id, meta)
		return err
	})
	return value, err
}

func (s *MongoStore) publishWithCommit(ctx context.Context, id string, meta CommitMeta) (model.Definition, error) {
	var draft bson.M
	if err := s.definitions.FindOne(ctx, bson.M{"definition_id": id, "version": 0}).Decode(&draft); errors.Is(err, mongo.ErrNoDocuments) {
		return model.Definition{}, ErrNotFound
	} else if err != nil {
		return model.Definition{}, err
	}
	latest := 0
	var published bson.M
	err := s.definitions.FindOne(ctx, bson.M{"definition_id": id, "version": bson.M{"$gt": 0}}, options.FindOne().SetSort(bson.D{{Key: "version", Value: -1}})).Decode(&published)
	if err == nil {
		latest = intValue(published["version"])
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return model.Definition{}, err
	}
	var value model.Definition
	if err := fromJSONValue(draft["body"], &value); err != nil {
		return value, err
	}
	value.Version = latest + 1
	value, err = definition.Normalize(value)
	if err != nil {
		return value, err
	}
	body, err := jsonDocument(value)
	if err != nil {
		return value, err
	}
	_, err = s.definitions.InsertOne(ctx, bson.M{"definition_id": id, "version": value.Version, "body": body, "created_at": utcNow()})
	if mongo.IsDuplicateKeyError(err) {
		return value, fmt.Errorf("published versions are immutable")
	}
	if err != nil {
		return value, err
	}
	version := value.Version
	if meta.Message == "" {
		meta.Message = fmt.Sprintf("Published version %d", version)
	}
	_, err = s.insertCommit(ctx, value, meta, "publish", &version, "")
	return value, err
}

func (s *MongoStore) Get(id string, version *int) (model.Definition, error) {
	filter := bson.M{"definition_id": id, "version": bson.M{"$gt": 0}}
	opts := options.FindOne().SetSort(bson.D{{Key: "version", Value: -1}})
	if version != nil {
		filter["version"] = *version
		opts = options.FindOne()
	}
	ctx, cancel := mongoContext()
	defer cancel()
	var document bson.M
	if err := s.definitions.FindOne(ctx, filter, opts).Decode(&document); errors.Is(err, mongo.ErrNoDocuments) {
		return model.Definition{}, ErrNotFound
	} else if err != nil {
		return model.Definition{}, err
	}
	var value model.Definition
	if err := fromJSONValue(document["body"], &value); err != nil {
		return value, err
	}
	return value, nil
}

func (s *MongoStore) List() ([]Summary, error) {
	ctx, cancel := mongoContext()
	defer cancel()
	cursor, err := s.definitions.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "definition_id", Value: 1}, {Key: "version", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	records := []definitionRecord{}
	for cursor.Next(ctx) {
		var document bson.M
		if err := cursor.Decode(&document); err != nil {
			return nil, err
		}
		var value model.Definition
		if err := fromJSONValue(document["body"], &value); err != nil {
			return nil, err
		}
		records = append(records, definitionRecord{ID: document["definition_id"].(string), Version: intValue(document["version"]), Body: value})
	}
	return summarizeDefinitions(records), cursor.Err()
}

func (s *MongoStore) insertCommit(ctx context.Context, value model.Definition, meta CommitMeta, kind string, publishedVersion *int, restoredFrom string) (DefinitionCommit, error) {
	var counter bson.M
	err := s.counters.FindOneAndUpdate(ctx,
		bson.M{"definition_id": value.ID},
		bson.M{"$inc": bson.M{"sequence": 1}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&counter)
	if err != nil {
		return DefinitionCommit{}, err
	}
	body := value
	commit := normalizeCommit(DefinitionCommit{CommitID: uuid.NewString(), DefinitionID: value.ID, Sequence: int64Value(counter["sequence"]), Kind: kind, Actor: meta.Actor, Message: meta.Message, PublishedVersion: publishedVersion, RestoredFromCommitID: restoredFrom, Body: &body})
	document, err := jsonDocument(commit)
	if err != nil {
		return commit, err
	}
	_, err = s.commits.InsertOne(ctx, document)
	return commit, err
}

func (s *MongoStore) ListCommits(definitionID string, limit int) ([]DefinitionCommit, error) {
	ctx, cancel := mongoContext()
	defer cancel()
	cursor, err := s.commits.Find(ctx,
		bson.M{"definition_id": definitionID},
		options.Find().SetProjection(bson.M{"_id": 0, "body": 0}).SetSort(bson.D{{Key: "sequence", Value: -1}}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	values := []DefinitionCommit{}
	for cursor.Next(ctx) {
		var document bson.M
		if err := cursor.Decode(&document); err != nil {
			return nil, err
		}
		var value DefinitionCommit
		if err := fromJSONValue(document, &value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, cursor.Err()
}

func (s *MongoStore) GetCommit(definitionID, commitID string) (DefinitionCommit, error) {
	ctx, cancel := mongoContext()
	defer cancel()
	var document bson.M
	if err := s.commits.FindOne(ctx, bson.M{"definition_id": definitionID, "commit_id": commitID}, options.FindOne().SetProjection(bson.M{"_id": 0})).Decode(&document); errors.Is(err, mongo.ErrNoDocuments) {
		return DefinitionCommit{}, ErrNotFound
	} else if err != nil {
		return DefinitionCommit{}, err
	}
	var value DefinitionCommit
	if err := fromJSONValue(document, &value); err != nil {
		return value, err
	}
	return value, nil
}

func (s *MongoStore) RestoreCommit(definitionID, commitID string, meta CommitMeta) (model.Definition, DefinitionCommit, error) {
	source, err := s.GetCommit(definitionID, commitID)
	if err != nil {
		return model.Definition{}, DefinitionCommit{}, err
	}
	if source.Body == nil {
		return model.Definition{}, DefinitionCommit{}, fmt.Errorf("commit body is missing")
	}
	value := *source.Body
	value.Version = 0
	value, err = definition.Draft(value)
	if err != nil {
		return value, DefinitionCommit{}, err
	}
	if meta.Message == "" {
		meta.Message = fmt.Sprintf("Restored commit %d", source.Sequence)
	}
	var commit DefinitionCommit
	body, err := jsonDocument(value)
	if err != nil {
		return value, commit, err
	}
	err = s.transaction(func(ctx context.Context) error {
		var err error
		commit, err = s.insertCommit(ctx, value, meta, "restore", nil, commitID)
		if err != nil {
			return err
		}
		_, err = s.definitions.ReplaceOne(ctx,
			bson.M{"definition_id": definitionID, "version": 0},
			bson.M{"definition_id": definitionID, "version": 0, "body": body, "created_at": utcNow()},
			options.Replace().SetUpsert(true))
		return err
	})
	return value, commit, err
}

func (s *MongoStore) transaction(fn func(context.Context) error) error {
	ctx, cancel := mongoContext()
	defer cancel()
	session, err := s.client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(ctx context.Context) (any, error) {
		return nil, fn(ctx)
	}, options.Transaction().SetReadConcern(readconcern.Snapshot()).SetWriteConcern(writeconcern.Majority()))
	return err
}

func (s *MongoStore) RecordRunStart(value RunRecord) (RunRecord, error) {
	value = normalizeRun(value)
	document, err := protectRun(value)
	if err != nil {
		return value, err
	}
	err = s.transaction(func(ctx context.Context) error {
		if _, err := s.runs.InsertOne(ctx, document); err != nil {
			return err
		}
		_, err := s.appendAudit(ctx, AuditEvent{WorkflowID: value.WorkflowID, DefinitionID: value.DefinitionID, EventType: "run.recorded", Actor: "system"})
		return err
	})
	return value, err
}

func (s *MongoStore) UpdateRun(workflowID, temporalStatus string, state *model.RunState) (bool, error) {
	return s.ObserveRun(context.Background(), workflowID, temporalStatus, state, time.Now().UTC())
}

func (s *MongoStore) ObserveRun(ctx context.Context, workflowID, temporalStatus string, state *model.RunState, observed time.Time) (bool, error) {
	var changed bool
	err := s.transaction(func(txctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		changed, err = s.observeRun(txctx, workflowID, temporalStatus, state, observed)
		return err
	})
	return changed && err == nil, err
}

func (s *MongoStore) observeRun(ctx context.Context, workflowID, temporalStatus string, state *model.RunState, observed time.Time) (bool, error) {
	observed = observed.UTC().Truncate(time.Millisecond)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var existing bson.M
	if err := s.runs.FindOne(ctx, bson.M{"workflow_id": workflowID}).Decode(&existing); errors.Is(err, mongo.ErrNoDocuments) {
		return false, ErrNotFound
	} else if err != nil {
		return false, err
	}
	var previousObserved time.Time
	if at, ok := existing["observed_at"].(bson.DateTime); ok {
		previousObserved = at.Time()
	}
	previousStatus, _ := existing["temporal_status"].(string)
	if !acceptObservation(previousStatus, previousObserved, temporalStatus, observed) {
		return false, nil
	}
	var previousState *model.RunState
	if existing["state"] != nil {
		previousState = &model.RunState{}
		if err := unprotectValue("run/"+workflowID+"/state", existing["state"], &previousState); err != nil {
			return false, err
		}
	}
	if state == nil {
		state = previousState
	}
	changed := previousStatus != temporalStatus || !statesEqual(previousState, state)
	stateValue, err := protectedValue("run/"+workflowID+"/state", state)
	if err != nil {
		return false, err
	}
	var output any
	if state != nil {
		output = state.Output
	}
	output, err = protectedValue("run/"+workflowID+"/output", output)
	if err != nil {
		return false, err
	}
	updates := bson.M{"observed_at": observed, "temporal_status": temporalStatus, "state": stateValue, "output": output, "updated_at": utcNow()}
	if finished(temporalStatus) && existing["completed_at"] == nil {
		updates["completed_at"] = updates["updated_at"]
	}
	result, err := s.runs.UpdateOne(ctx, bson.M{"workflow_id": workflowID, "observed_at": existing["observed_at"], "temporal_status": existing["temporal_status"]}, bson.M{"$set": updates})
	if err == nil && changed && result.ModifiedCount == 1 {
		_, err = s.appendAudit(ctx, AuditEvent{WorkflowID: workflowID, EventType: "run.observed", Actor: "system", Payload: map[string]any{"status": temporalStatus}})
	}
	return changed && err == nil && result.ModifiedCount == 1, err
}

func (s *MongoStore) GetRun(workflowID string) (RunRecord, error) {
	ctx, cancel := mongoContext()
	defer cancel()
	var document bson.M
	if err := s.runs.FindOne(ctx, bson.M{"workflow_id": workflowID}, options.FindOne().SetProjection(bson.M{"_id": 0})).Decode(&document); errors.Is(err, mongo.ErrNoDocuments) {
		return RunRecord{}, ErrNotFound
	} else if err != nil {
		return RunRecord{}, err
	}
	var value RunRecord
	if err := readProtectedRun(document, &value); err != nil {
		return value, err
	}
	return value, nil
}

func (s *MongoStore) ListRuns(limit int) ([]RunRecord, error) {
	ctx, cancel := mongoContext()
	defer cancel()
	cursor, err := s.runs.Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{"_id": 0}).SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	values := []RunRecord{}
	for cursor.Next(ctx) {
		var document bson.M
		if err := cursor.Decode(&document); err != nil {
			return nil, err
		}
		var value RunRecord
		if err := readProtectedRun(document, &value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, cursor.Err()
}

func (s *MongoStore) AppendAudit(value AuditEvent) (AuditEvent, error) {
	ctx, cancel := mongoContext()
	defer cancel()
	return s.appendAudit(ctx, value)
}

func (s *MongoStore) appendAudit(ctx context.Context, value AuditEvent) (AuditEvent, error) {
	value = normalizeAudit(value)
	document, err := protectAudit(value)
	if err != nil {
		return value, err
	}
	_, err = s.audits.InsertOne(ctx, document)
	// The event_id unique index makes replaying an already recorded event a no-op.
	if mongo.IsDuplicateKeyError(err) {
		return value, nil
	}
	return value, err
}

func (s *MongoStore) ListAudit(workflowID, definitionID string, limit int) ([]AuditEvent, error) {
	filter := bson.M{}
	if workflowID != "" {
		filter["workflow_id"] = workflowID
	}
	if definitionID != "" {
		filter["definition_id"] = definitionID
	}
	ctx, cancel := mongoContext()
	defer cancel()
	cursor, err := s.audits.Find(ctx, filter, options.Find().SetProjection(bson.M{"_id": 0}).SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	values := []AuditEvent{}
	for cursor.Next(ctx) {
		var document bson.M
		if err := cursor.Decode(&document); err != nil {
			return nil, err
		}
		var value AuditEvent
		if err := readProtectedAudit(document, &value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, cursor.Err()
}

func (s *MongoStore) Health(ctx context.Context) bool { return s.client.Ping(ctx, nil) == nil }
func (s *MongoStore) Close(ctx context.Context) error { return s.client.Disconnect(ctx) }

func mongoContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

func jsonDocument(value any) (bson.M, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var document bson.M
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	return document, nil
}

func jsonValue(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var normalized any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}

func fromJSONValue(value, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func intValue(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int32:
		return int(number)
	case int64:
		return int(number)
	case float64:
		return int(number)
	default:
		return 0
	}
}

func int64Value(value any) int64 {
	switch number := value.(type) {
	case int:
		return int64(number)
	case int32:
		return int64(number)
	case int64:
		return number
	case float64:
		return int64(number)
	default:
		return 0
	}
}

func statesEqual(left, right *model.RunState) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}
