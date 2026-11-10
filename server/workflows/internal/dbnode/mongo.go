package dbnode

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (e Executor) openMongo(c Connection) (*mongo.Client, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := e.Network.Check(c.Host, c.Port, c.TLS); err != nil {
		return nil, err
	}
	host := net.JoinHostPort(c.Host, fmt.Sprint(c.Port))
	scheme := "mongodb"
	if c.SRV {
		host = c.Host
		scheme = "mongodb+srv"
	}
	u := url.URL{Scheme: scheme, Host: host, Path: "/" + c.Database, User: url.UserPassword(c.Username, c.Password)}
	q := u.Query()
	authSource := c.AuthSource
	if authSource == "" {
		authSource = "admin"
	}
	q.Set("authSource", authSource)
	u.RawQuery = q.Encode()
	opts := options.Client().ApplyURI(u.String()).SetDialer(e.Network).SetMaxPoolSize(1).SetMinPoolSize(0).SetRetryWrites(false).SetRetryReads(false).SetConnectTimeout(5 * time.Second).SetServerSelectionTimeout(5 * time.Second).SetTimeout(20 * time.Second)
	if c.TLS {
		opts.SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})
	} else {
		opts.SetTLSConfig(nil)
	}
	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, connectionError(err, c)
	}
	return client, nil
}
func closeMongo(c *mongo.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.Disconnect(ctx)
}
func mongoFilter(filter map[string]any) bson.M {
	m := bson.M{}
	for k, v := range filter {
		m[k] = bson.M{"$eq": v}
	}
	return m
}
func safePipeline(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		for k, v := range t {
			switch k {
			case "$where", "$function", "$accumulator", "$out", "$merge":
				return false
			}
			if !safePipeline(v) {
				return false
			}
		}
	case []any:
		for _, v := range t {
			if !safePipeline(v) {
				return false
			}
		}
	}
	return true
}
func (e Executor) mongoExecute(ctx context.Context, c Connection, o Operation) (Result, error) {
	result := Result{Rows: []map[string]any{}}
	if o.Operation == "sql" {
		return result, fmt.Errorf("MongoDB uses JSON operations, not SQL")
	}
	if c.Access == "read" && o.Operation != "select" && o.Operation != "aggregate" {
		return result, fmt.Errorf("connection is read-only")
	}
	for _, stage := range o.Pipeline {
		if !safePipeline(stage) {
			return result, fmt.Errorf("server-side code and write aggregation stages are not supported")
		}
	}
	client, err := e.openMongo(c)
	if err != nil {
		return result, err
	}
	defer closeMongo(client)
	collection := client.Database(c.Database).Collection(o.Table)
	limit := o.Limit
	if limit == 0 {
		limit = 100
	}
	var cursor *mongo.Cursor
	switch o.Operation {
	case "select":
		cursor, err = collection.Find(ctx, mongoFilter(o.Filter), options.Find().SetLimit(int64(limit+1)).SetBatchSize(50))
	case "aggregate":
		pipeline := mongo.Pipeline{}
		for _, s := range o.Pipeline {
			for k, v := range s {
				pipeline = append(pipeline, bson.D{{Key: k, Value: v}})
			}
		}
		pipeline = append(pipeline, bson.D{{Key: "$limit", Value: limit + 1}})
		cursor, err = collection.Aggregate(ctx, pipeline, options.Aggregate().SetAllowDiskUse(false).SetBatchSize(50))
	case "insert":
		_, err = collection.InsertOne(ctx, o.Values)
		if err == nil {
			result.Affected = 1
		}
	case "update":
		var r *mongo.UpdateResult
		r, err = collection.UpdateMany(ctx, mongoFilter(o.Filter), bson.M{"$set": o.Values})
		if err == nil {
			result.Affected = r.ModifiedCount
		}
	case "delete":
		var r *mongo.DeleteResult
		r, err = collection.DeleteMany(ctx, mongoFilter(o.Filter))
		if err == nil {
			result.Affected = r.DeletedCount
		}
	}
	if err != nil {
		return result, fmt.Errorf("MongoDB operation failed; verify connection, input and permissions; a write may already have applied")
	}
	if cursor != nil {
		defer cursor.Close(ctx)
		for cursor.Next(ctx) {
			if len(result.Rows) >= limit {
				result.Truncated = true
				break
			}
			var row map[string]any
			if cursor.Decode(&row) != nil {
				return Result{}, fmt.Errorf("MongoDB result cannot be decoded")
			}
			result.Rows = append(result.Rows, row)
			raw, err := json.Marshal(result)
			if err != nil || len(raw) > 256*1024 {
				return Result{}, fmt.Errorf("database result exceeds 256 KiB; select fewer rows or fields")
			}
		}
		if cursor.Err() != nil {
			return Result{}, fmt.Errorf("MongoDB result interrupted")
		}
	}
	return result, nil
}
func (e Executor) mongoDiscover(ctx context.Context, c Connection) ([]Table, error) {
	client, err := e.openMongo(c)
	if err != nil {
		return nil, err
	}
	defer closeMongo(client)
	db := client.Database(c.Database)
	cursor, err := db.ListCollections(ctx, bson.M{})
	if err != nil {
		return nil, connectionError(err, c)
	}
	defer cursor.Close(ctx)
	tables := []Table{}
	for cursor.Next(ctx) {
		if len(tables) >= 100 {
			return nil, fmt.Errorf("discovery is limited to 100 collections")
		}
		var record struct {
			Name string `bson:"name"`
		}
		if cursor.Decode(&record) != nil {
			return nil, fmt.Errorf("collection discovery failed")
		}
		if !identifier.MatchString(record.Name) {
			continue
		}
		// Schema is sampled, not a MongoDB validation contract. Values never leave this method.
		rows, err := db.Collection(record.Name).Find(ctx, bson.M{}, options.Find().SetLimit(5))
		if err != nil {
			return nil, connectionError(err, c)
		}
		fields := map[string]string{}
		for rows.Next(ctx) {
			var doc bson.M
			if rows.Decode(&doc) != nil {
				rows.Close(ctx)
				return nil, fmt.Errorf("cannot sample collection fields")
			}
			for k, v := range doc {
				if !identifier.MatchString(k) {
					continue
				}
				kind := fmt.Sprintf("%T", v)
				if old, ok := fields[k]; ok && old != kind {
					kind = "mixed"
				}
				fields[k] = kind
			}
		}
		err = rows.Err()
		rows.Close(ctx)
		if err != nil {
			return nil, fmt.Errorf("collection sampling interrupted")
		}
		t := Table{Schema: "", Name: record.Name, Columns: []Column{}, Inferred: true}
		names := []string{}
		for k := range fields {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			t.Columns = append(t.Columns, Column{Name: k, Type: fields[k], Nullable: true})
		}
		tables = append(tables, t)
	}
	if cursor.Err() != nil {
		return nil, fmt.Errorf("collection discovery interrupted")
	}
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	return tables, nil
}
func (e Executor) Test(ctx context.Context, c Connection) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if !acquire(ctx) {
		return fmt.Errorf("database connector capacity exhausted")
	}
	defer release()
	if !c.Enabled {
		return fmt.Errorf("connection is disabled")
	}
	if c.Provider == "mongodb" {
		db, err := e.openMongo(c)
		if err != nil {
			return err
		}
		defer closeMongo(db)
		if err := db.Ping(ctx, nil); err != nil {
			return connectionError(err, c)
		}
		return nil
	}
	db, err := e.openSQL(c)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return connectionError(err, c)
	}
	return nil
}
func (e Executor) Discover(ctx context.Context, c Connection) ([]Table, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if !acquire(ctx) {
		return nil, fmt.Errorf("database connector capacity exhausted")
	}
	defer release()
	if !c.Enabled {
		return nil, fmt.Errorf("connection is disabled")
	}
	if c.Provider == "mongodb" {
		return e.mongoDiscover(ctx, c)
	}
	return e.sqlDiscover(ctx, c)
}
func (e Executor) Execute(ctx context.Context, c Connection, o Operation) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if !acquire(ctx) {
		return Result{}, fmt.Errorf("database connector capacity exhausted")
	}
	defer release()
	if err := Validate(&o); err != nil {
		return Result{}, err
	}
	if !c.Enabled {
		return Result{}, fmt.Errorf("connection is disabled")
	}
	if c.Provider == "mongodb" {
		return e.mongoExecute(ctx, c, o)
	}
	return e.sqlExecute(ctx, c, o)
}
