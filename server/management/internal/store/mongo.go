package store

import (
	"context"

	"github.com/example/runloom/management/internal/recordset"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type MongoStore struct {
	*promptStore
	*projectStore
	projects *mongo.Collection
	client   *mongo.Client
	database *mongo.Database
	usage    *mongo.Collection
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
		projects: database.Collection(prefix + "metadata_entities"),
		client:   client,
		database: database,
		usage:    database.Collection(prefix + "llm_usage"),
	}
	_, err = store.projects.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "scope", Value: 1}, {Key: "key", Value: 1}}})
	if err != nil {
		client.Disconnect(context.Background())
		return nil, err
	}
	store.projectStore = newProjectStore(store.readProjects, store.swapProjects, nil)
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
	return nil
}
func (s *MongoStore) Driver() string { return "mongodb" }

func (s *MongoStore) Health(ctx context.Context) bool { return s.client.Ping(ctx, nil) == nil }

func (s *MongoStore) Close(ctx context.Context) error { return s.client.Disconnect(ctx) }
