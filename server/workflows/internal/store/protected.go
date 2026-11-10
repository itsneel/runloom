package store

import (
	"encoding/json"
	"github.com/example/runloom/workflows/internal/protection"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Bind payload authentication to the immutable audit identity and actor. This
// detects edits/substitution, not deletion or rollback of complete audit records.
func auditContext(v AuditEvent) string {
	raw, _ := json.Marshal([]string{v.EventID, v.WorkflowID, v.DefinitionID, v.EventType, v.StepID, v.Actor})
	return "audit/" + string(raw)
}
func protectedValue(context string, v any) (any, error) {
	raw, err := protection.Marshal(context, v)
	if err != nil {
		return nil, err
	}
	var out any
	err = json.Unmarshal(raw, &out)
	return out, err
}
func unprotectValue(context string, v, target any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return protection.Unmarshal(context, raw, target)
}
func protectRun(v RunRecord) (bson.M, error) {
	doc, err := jsonDocument(v)
	if err != nil {
		return nil, err
	}
	for _, field := range []string{"input", "state", "output"} {
		doc[field], err = protectedValue("run/"+v.WorkflowID+"/"+field, doc[field])
		if err != nil {
			return nil, err
		}
	}
	return doc, nil
}
func readProtectedRun(doc bson.M, v *RunRecord) error {
	id, _ := doc["workflow_id"].(string)
	for _, field := range []string{"input", "state", "output"} {
		var value any
		if err := unprotectValue("run/"+id+"/"+field, doc[field], &value); err != nil {
			return err
		}
		doc[field] = value
	}
	return fromJSONValue(doc, v)
}
func protectAudit(v AuditEvent) (bson.M, error) {
	doc, err := jsonDocument(v)
	if err != nil {
		return nil, err
	}
	doc["payload"], err = protectedValue(auditContext(v), v.Payload)
	return doc, err
}
func readProtectedAudit(doc bson.M, v *AuditEvent) error {
	if err := fromJSONValue(doc, v); err != nil {
		return err
	}
	return unprotectValue(auditContext(*v), doc["payload"], &v.Payload)
}
