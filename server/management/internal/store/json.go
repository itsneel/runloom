package store

import (
	"encoding/json"
	"go.mongodb.org/mongo-driver/v2/bson"
)

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
func fromJSONValue(value, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
