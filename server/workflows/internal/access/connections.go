package access

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"github.com/example/runloom/workflows/internal/dbnode"
	"time"
)

type StoredConnection struct {
	TeamID     string            `json:"team_id"`
	Connection dbnode.Connection `json:"connection"`
	Ciphertext string            `json:"ciphertext"`
}

func (v *SecretVault) PutConnection(ctx context.Context, u User, org, team string, c dbnode.Connection) error {
	if v == nil {
		return fmt.Errorf("connection vault is not configured")
	}
	if err := c.Validate(); err != nil {
		return invalid(err.Error())
	}
	key := team + "/" + c.ID
	return v.Directory.UpdateModule(ctx, "secrets", func(s *State) error {
		actual, role := TeamRole(*s, team, u.ID)
		if actual != org || role != "admin" {
			return ErrForbidden
		}
		if s.Connections == nil {
			s.Connections = map[string]StoredConnection{}
		}
		previous, exists := s.Connections[key]
		if !exists && len(s.Connections) >= 500 {
			return invalid("connection limit reached")
		}
		ciphertext := previous.Ciphertext
		if c.Password != "" || !exists {
			if c.Password == "" {
				return invalid("password is required for a new connection")
			}
			nonce := make([]byte, v.cipher.NonceSize())
			if _, err := rand.Read(nonce); err != nil {
				return err
			}
			ciphertext = base64.StdEncoding.EncodeToString(v.cipher.Seal(nonce, nonce, []byte(c.Password), []byte("database:"+key)))
		}
		// The optimistic update can retry. Never clear the captured input password
		// before a successful swap, or a concurrent update could lose a rotation.
		public := c.Public()
		public.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		s.Connections[key] = StoredConnection{TeamID: team, Connection: public, Ciphertext: ciphertext}
		s.Events = append(s.Events, Event{OrgID: org, Actor: u.ID, Action: "connection.saved", Target: key, CreatedAt: public.UpdatedAt})
		return nil
	})
}
func (v *SecretVault) ResolveConnection(ctx context.Context, team, id string) (dbnode.Connection, error) {
	if v == nil || team == "" || !dbnode.ID.MatchString(id) {
		return dbnode.Connection{}, fmt.Errorf("connection is unavailable")
	}
	s, _, err := v.Directory.Repo.Module("secrets").Read(ctx)
	if err != nil {
		return dbnode.Connection{}, fmt.Errorf("connection storage unavailable")
	}
	key := team + "/" + id
	stored, ok := s.Connections[key]
	if !ok || stored.TeamID != team {
		return dbnode.Connection{}, fmt.Errorf("connection is unavailable for this team")
	}
	raw, err := base64.StdEncoding.DecodeString(stored.Ciphertext)
	if err != nil || len(raw) < v.cipher.NonceSize() {
		return dbnode.Connection{}, fmt.Errorf("connection credential cannot be decrypted")
	}
	value, err := v.cipher.Open(nil, raw[:v.cipher.NonceSize()], raw[v.cipher.NonceSize():], []byte("database:"+key))
	if err != nil {
		return dbnode.Connection{}, fmt.Errorf("connection credential cannot be decrypted")
	}
	c := stored.Connection
	c.Password = string(value)
	return c, nil
}
