// database-setup provisions fresh service databases. It never deletes data or rotates credentials.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type settings struct {
	Assistant    bool   `json:"assistant"`
	Driver       string `json:"driver"`
	AdminURLFile string `json:"adminURLFile"`
	Directory    string `json:"directory"`
}

func main() {
	if err := run(); err != nil {
		// Driver errors can contain credentials or statements. Never print them.
		fmt.Fprintln(os.Stderr, "Service database setup failed; check configuration and existing credential ownership. No databases were deleted or credentials rotated.")
		var command mongo.CommandError
		if errors.As(err, &command) {
			fmt.Fprintf(os.Stderr, "MongoDB error code: %d\n", command.Code)
		}
		var file *os.PathError
		if errors.As(err, &file) {
			fmt.Fprintln(os.Stderr, "Credential/configuration file access failed.")
		}
		os.Exit(1)
	}
	fmt.Println("Service databases provisioned; owner authentication and cross-database isolation verified.")
}

func read(path string) (string, error) {
	b, e := os.ReadFile(path)
	return strings.TrimSpace(string(b)), e
}

func run() error {
	var cfg settings
	b, err := os.ReadFile("/etc/runloom/database-setup.json")
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &cfg); err != nil {
		return err
	}
	adminURL, err := read(cfg.AdminURLFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	owners := []string{"auth", "workflows", "management"}
	if cfg.Assistant {
		owners = append(owners, "assistant")
	}
	if cfg.Driver == "postgres" {
		admin, err := pgx.Connect(ctx, adminURL)
		if err != nil {
			return err
		}
		defer admin.Close(ctx)
		for _, owner := range owners {
			name := "runloom_" + owner
			password, err := read(cfg.Directory + "/" + owner + "-database-password")
			if err != nil {
				return err
			}
			if len(password) < 32 {
				return fmt.Errorf("short password")
			}
			var exists bool
			if err = admin.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)", name).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				_, err = admin.Exec(ctx, "CREATE ROLE "+pgx.Identifier{name}.Sanitize()+" LOGIN PASSWORD '"+strings.ReplaceAll(password, "'", "''")+"'")
				if err != nil {
					return err
				}
			}
			if err = admin.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)", name).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				_, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()+" OWNER "+pgx.Identifier{name}.Sanitize())
				if err != nil {
					return err
				}
			}
			var actualOwner string
			if err = admin.QueryRow(ctx, "SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname=$1", name).Scan(&actualOwner); err != nil || actualOwner != name {
				return fmt.Errorf("unexpected database owner")
			}
			if _, err = admin.Exec(ctx, "REVOKE ALL ON DATABASE "+pgx.Identifier{name}.Sanitize()+" FROM PUBLIC"); err != nil {
				return err
			}
		}
		for _, owner := range owners {
			ownerURL, err := read(cfg.Directory + "/" + owner + "-database-url")
			if err != nil {
				return err
			}
			conn, err := pgx.Connect(ctx, ownerURL)
			if err != nil {
				return err
			}
			if err = conn.Ping(ctx); err != nil {
				conn.Close(ctx)
				return err
			}
			conn.Close(ctx)
			for _, other := range owners {
				if other == owner {
					continue
				}
				u, err := url.Parse(ownerURL)
				if err != nil {
					return err
				}
				u.Path = "/runloom_" + other
				cross, e := pgx.Connect(ctx, u.String())
				if e == nil {
					cross.Close(ctx)
					return fmt.Errorf("cross database access allowed")
				}
			}
		}
		return nil
	}
	if cfg.Driver != "mongodb" {
		return fmt.Errorf("unsupported database")
	}
	admin, err := mongo.Connect(options.Client().ApplyURI(adminURL))
	if err != nil {
		return err
	}
	defer admin.Disconnect(ctx)
	for _, owner := range owners {
		name := "runloom_" + owner
		db := admin.Database(name)
		password, err := read(cfg.Directory + "/" + owner + "-database-password")
		if err != nil {
			return err
		}
		if len(password) < 32 {
			return fmt.Errorf("short password")
		}
		var result struct {
			Users []bson.M `bson:"users"`
		}
		if err = db.RunCommand(ctx, bson.D{{Key: "usersInfo", Value: name}}).Decode(&result); err != nil {
			return err
		}
		if len(result.Users) == 0 {
			err = db.RunCommand(ctx, bson.D{{Key: "createUser", Value: name}, {Key: "pwd", Value: password}, {Key: "roles", Value: bson.A{bson.D{{Key: "role", Value: "readWrite"}, {Key: "db", Value: name}}}}}).Err()
			if err != nil {
				return err
			}
		}
		ownerURL, err := read(cfg.Directory + "/" + owner + "-database-url")
		if err != nil {
			return err
		}
		conn, err := mongo.Connect(options.Client().ApplyURI(ownerURL))
		if err != nil {
			return err
		}
		_, err = conn.Database(name).ListCollectionNames(ctx, bson.D{})
		if err != nil {
			conn.Disconnect(ctx)
			return err
		}
		for _, other := range owners {
			if other == owner {
				continue
			}
			_, e := conn.Database("runloom_"+other).ListCollectionNames(ctx, bson.D{})
			if e == nil {
				conn.Disconnect(ctx)
				return fmt.Errorf("cross database access allowed")
			}
		}
		conn.Disconnect(ctx)
	}
	return nil
}
