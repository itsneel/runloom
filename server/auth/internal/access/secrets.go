package access

type StoredSecret struct {
	Name   string `json:"name"`
	TeamID string `json:"team_id"`
	// LegacyOrigin is only used to decrypt records saved before origin restrictions were removed.
	LegacyOrigin string `json:"origin,omitempty"`
	UpdatedAt    string `json:"updated_at"`
	Ciphertext   string `json:"ciphertext"`
}
