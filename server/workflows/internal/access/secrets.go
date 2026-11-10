package access

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

type StoredSecret struct {
	Name   string `json:"name"`
	TeamID string `json:"team_id"`
	// LegacyOrigin is only used to decrypt records saved before origin restrictions were removed.
	LegacyOrigin string `json:"origin,omitempty"`
	UpdatedAt    string `json:"updated_at"`
	Ciphertext   string `json:"ciphertext"`
}

type SecretVault struct {
	Directory *Directory
	cipher    cipher.AEAD
}

// The 32-byte key is provisioned separately from the database and is never generated implicitly.
func NewSecretVault(d *Directory, file string) (*SecretVault, error) {
	if file == "" {
		return nil, nil
	}
	key, err := os.ReadFile(file)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("workflow secret key file must contain exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("invalid workflow secret key")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &SecretVault{Directory: d, cipher: aead}, nil
}

var secretName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func (v *SecretVault) Put(ctx context.Context, actor User, org, team, name, value string) error {
	if !secretName.MatchString(name) || len(value) < 1 || len(value) > 8192 || strings.ContainsAny(value, "\r\n\x00") {
		return invalid("use a lowercase secret name starting with a letter (letters, digits and hyphens, up to 64 characters) and a value of 1–8192 bytes without newlines or NUL")
	}
	key := team + "/" + name
	nonce := make([]byte, v.cipher.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	sealed := v.cipher.Seal(nonce, nonce, []byte(value), []byte(key+"\x00"))
	return v.Directory.UpdateModule(ctx, "secrets", func(s *State) error {
		actualOrg, role := TeamRole(*s, team, actor.ID)
		if org != actualOrg || role != "admin" {
			return ErrForbidden
		}
		if s.Secrets == nil {
			s.Secrets = map[string]StoredSecret{}
		}
		if _, exists := s.Secrets[key]; !exists && len(s.Secrets) >= 500 {
			return invalid("secret directory limit reached (500)")
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		s.Secrets[key] = StoredSecret{Name: name, TeamID: team, UpdatedAt: now, Ciphertext: base64.StdEncoding.EncodeToString(sealed)}
		s.Events = append(s.Events, Event{OrgID: org, Actor: actor.ID, Action: "secret.saved", Target: team + "/" + name, CreatedAt: now})
		return nil
	})
}

func (v *SecretVault) Resolve(ctx context.Context, team, name string) (string, error) {
	if v == nil || team == "" {
		return "", fmt.Errorf("workflow secrets are not configured")
	}
	s, _, err := v.Directory.Repo.Module("secrets").Read(ctx)
	if err != nil {
		return "", fmt.Errorf("secret storage unavailable")
	}
	key := team + "/" + name
	secret, ok := s.Secrets[key]
	if !ok || secret.TeamID != team {
		return "", fmt.Errorf("secret unavailable for this team")
	}
	sealed, err := base64.StdEncoding.DecodeString(secret.Ciphertext)
	if err != nil || len(sealed) < v.cipher.NonceSize() {
		return "", fmt.Errorf("secret cannot be decrypted")
	}
	value, err := v.cipher.Open(nil, sealed[:v.cipher.NonceSize()], sealed[v.cipher.NonceSize():], []byte(key+"\x00"+secret.LegacyOrigin))
	if err != nil {
		return "", fmt.Errorf("secret cannot be decrypted")
	}
	return string(value), nil
}
