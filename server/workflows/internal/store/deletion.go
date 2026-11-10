package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var ErrPublishedWorkflow = errors.New("only workflows that have never been published can be deleted")

// DeleteDraft removes the current definition and project membership, not commit,
// audit or run history. The API also reserves the existing editing lease.
func (s *Store) DeleteDraft(ctx context.Context, id string) error {
	if !projectDefinitionID.MatchString(id) {
		return ErrNotFound
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	versions, err := s.versions(id)
	if err != nil {
		return err
	}
	if len(versions) > 0 {
		return ErrPublishedWorkflow
	}
	if _, err := s.read(s.draftPath(id)); err != nil {
		return err
	}
	c, revision, err := s.readProjectsLocked("projects")
	if err != nil {
		return err
	}
	delete(c.WorkflowProjects, id)
	// File storage is a single-process development adapter. If removing the
	// file fails, the surviving draft is safely visible in General.
	if err = s.writeJSON(filepath.Join(s.root, "_projects", "projects.json"), projectDocument{ID: "projects", Revision: revision + 1, Body: c}); err != nil {
		return err
	}
	return os.Remove(s.draftPath(id))
}

func (s *PostgresStore) DeleteDraft(ctx context.Context, id string) error {
	if !projectDefinitionID.MatchString(id) {
		return ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Same per-definition lock as save, publish, restore and authoring.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, id); err != nil {
		return err
	}
	var published bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runloom_definitions WHERE definition_id=$1 AND version>0)`, id).Scan(&published); err != nil {
		return err
	}
	if published {
		return ErrPublishedWorkflow
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM runloom_definitions WHERE definition_id=$1 AND version=0`, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	if err = touchPostgresProjects(ctx, tx); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM runloom_entity_records WHERE scope='projects' AND key=$1`, "workflow_projects/map/"+id); err != nil {
		return err
	}
	return tx.Commit()
}

func touchPostgresProjects(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO runloom_entity_heads(scope,revision) VALUES('projects',1) ON CONFLICT(scope) DO UPDATE SET revision=runloom_entity_heads.revision+1`)
	return err
}

func (s *MongoStore) DeleteDraft(ctx context.Context, id string) error {
	if !projectDefinitionID.MatchString(id) {
		return ErrNotFound
	}
	session, err := s.client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(tx context.Context) (any, error) {
		// Save/publish/restore/authoring also write this counter. Touch it before
		// checking versions to prevent snapshot write skew with publication.
		if _, err := s.counters.UpdateOne(tx, bson.M{"definition_id": id}, bson.M{"$inc": bson.M{"mutation_revision": 1}}, options.UpdateOne().SetUpsert(true)); err != nil {
			return nil, err
		}
		published, err := s.definitions.CountDocuments(tx, bson.M{"definition_id": id, "version": bson.M{"$gt": 0}}, options.Count().SetLimit(1))
		if err != nil {
			return nil, err
		}
		if published > 0 {
			return nil, ErrPublishedWorkflow
		}
		result, err := s.definitions.DeleteOne(tx, bson.M{"definition_id": id, "version": 0})
		if err != nil {
			return nil, err
		}
		if result.DeletedCount == 0 {
			return nil, ErrNotFound
		}
		if _, err = s.projects.UpdateOne(tx, bson.M{"_id": "projects/head"}, bson.M{"$inc": bson.M{"revision": 1}, "$set": bson.M{"scope": "projects"}}, options.UpdateOne().SetUpsert(true)); err != nil {
			return nil, err
		}
		_, err = s.projects.DeleteOne(tx, bson.M{"_id": "projects/record/workflow_projects/map/" + id})
		return nil, err
	})
	return err
}
