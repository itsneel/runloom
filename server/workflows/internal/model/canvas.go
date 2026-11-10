package model

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Reject missing/null axes rather than silently moving a card to the origin.
func (p *CanvasPoint) UnmarshalJSON(data []byte) error {
	var point struct {
		X *float64 `json:"x"`
		Y *float64 `json:"y"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&point); err != nil {
		return err
	}
	if point.X == nil || point.Y == nil {
		return fmt.Errorf("layout coordinates require numeric x and y")
	}
	p.X, p.Y = *point.X, *point.Y
	return nil
}
