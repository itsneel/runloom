// Package protection encrypts execution data before it leaves the application.
// Keys are startup snapshots. Rotation retains previous keys for history replay.
package protection

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"sync"

	"github.com/example/runloom/workflows/internal/config"
)

var ErrProtectedData = errors.New("protected data unavailable or authentication failed")

type Ring struct {
	Active string
	Keys   map[string]cipher.AEAD
}
type envelope struct {
	Version int    `json:"_runloom_encrypted"`
	KeyID   string `json:"key_id"`
	Nonce   []byte `json:"nonce"`
	Data    []byte `json:"ciphertext"`
}

var cache sync.Map

// FromConfig accepts a mounted JSON keyring containing active and keys (base64
// 32-byte keys). Otherwise derive a separate execution key from the vault key.
// No network requests or credential values appear in errors.
func FromConfig() (*Ring, error) {
	path := config.Value("RUNLOOM_PAYLOAD_KEYRING_FILE")
	vault := config.Value("RUNLOOM_SECRETS_KEY_FILE")
	key := path + "\x00" + vault
	if v, ok := cache.Load(key); ok {
		return v.(*Ring), nil
	}
	if path == "" && vault == "" {
		return nil, nil
	}
	keys := map[string][]byte{}
	active := "vault-derived-v1"
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil || len(data) > 65536 {
			return nil, ErrProtectedData
		}
		var file struct {
			Active string            `json:"active"`
			Keys   map[string]string `json:"keys"`
		}
		if json.Unmarshal(data, &file) != nil || file.Active == "" {
			return nil, ErrProtectedData
		}
		active = file.Active
		for id, encoded := range file.Keys {
			raw, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return nil, ErrProtectedData
			}
			keys[id] = raw
		}
	} else {
		raw, err := os.ReadFile(vault)
		if err != nil || len(raw) != 32 {
			return nil, ErrProtectedData
		}
		mac := hmac.New(sha256.New, raw)
		mac.Write([]byte("runloom/execution-encryption/v1"))
		keys[active] = mac.Sum(nil)
	}
	ring, err := New(active, keys)
	if err != nil {
		return nil, err
	}
	cache.Store(key, ring)
	return ring, nil
}

func New(active string, keys map[string][]byte) (*Ring, error) {
	r := &Ring{Active: active, Keys: map[string]cipher.AEAD{}}
	for id, key := range keys {
		if id == "" || len(id) > 128 || len(key) != 32 {
			return nil, ErrProtectedData
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, ErrProtectedData
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, ErrProtectedData
		}
		r.Keys[id] = aead
	}
	if r.Keys[active] == nil {
		return nil, ErrProtectedData
	}
	return r, nil
}

func (r *Ring) Seal(context string, raw []byte) ([]byte, error) {
	if r == nil {
		return raw, nil
	}
	aead := r.Keys[r.Active]
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, ErrProtectedData
	}
	return json.Marshal(envelope{1, r.Active, nonce, aead.Seal(nil, nonce, raw, []byte(context+"\x00"+r.Active))})
}

func (r *Ring) Open(context string, raw []byte) ([]byte, error) {
	var e envelope
	// Legacy plaintext requires an explicit compatibility exception when keys
	// are configured. Ciphertexts never fall back on missing keys or corruption.
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields["_runloom_encrypted"] == nil {
		if r != nil && config.Value("RUNLOOM_ALLOW_LEGACY_PAYLOADS") != "true" {
			return nil, ErrProtectedData
		}
		return raw, nil
	}
	if json.Unmarshal(raw, &e) != nil || e.Version != 1 || r == nil {
		return nil, ErrProtectedData
	}
	aead := r.Keys[e.KeyID]
	if aead == nil || len(e.Nonce) != aead.NonceSize() {
		return nil, ErrProtectedData
	}
	value, err := aead.Open(nil, e.Nonce, e.Data, []byte(context+"\x00"+e.KeyID))
	if err != nil {
		return nil, ErrProtectedData
	}
	return value, nil
}

func Marshal(context string, value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, ErrProtectedData
	}
	ring, err := FromConfig()
	if err != nil {
		return nil, err
	}
	return ring.Seal(context, raw)
}
func Unmarshal(context string, raw []byte, value any) error {
	ring, err := FromConfig()
	if err != nil {
		return err
	}
	raw, err = ring.Open(context, raw)
	if err != nil {
		return err
	}
	if json.Unmarshal(raw, value) != nil {
		return ErrProtectedData
	}
	return nil
}
