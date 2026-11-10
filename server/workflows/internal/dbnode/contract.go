// Package dbnode implements external database connections, not Runloom's storage.
package dbnode

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"
)

type Connection struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Database   string `json:"database"`
	Username   string `json:"username"`
	Password   string `json:"password,omitempty"`
	TLS        bool   `json:"tls"`
	SRV        bool   `json:"srv,omitempty"`
	AuthSource string `json:"auth_source,omitempty"`
	Access     string `json:"access"`
	Enabled    bool   `json:"enabled"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

type Operation struct {
	ConnectionID string           `json:"connection_id"`
	Operation    string           `json:"operation"`
	Schema       string           `json:"schema,omitempty"`
	Table        string           `json:"table,omitempty"`
	Query        string           `json:"query,omitempty"`
	Parameters   []any            `json:"parameters,omitempty"`
	Values       map[string]any   `json:"values,omitempty"`
	Filter       map[string]any   `json:"filter,omitempty"`
	Pipeline     []map[string]any `json:"pipeline,omitempty"`
	Limit        int              `json:"limit,omitempty"`
}
type Input struct {
	TeamID    string    `json:"team_id"`
	Operation Operation `json:"operation"`
}
type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}
type Table struct {
	Schema   string   `json:"schema"`
	Name     string   `json:"name"`
	Columns  []Column `json:"columns"`
	Inferred bool     `json:"inferred,omitempty"`
}
type Result struct {
	Rows      []map[string]any `json:"rows"`
	Affected  int64            `json:"affected"`
	Truncated bool             `json:"truncated"`
}

var ID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var identifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,62}$`)

func (c Connection) Public() Connection { c.Password = ""; return c }
func (c Connection) Validate() error {
	if !ID.MatchString(c.ID) || len(strings.TrimSpace(c.Name)) == 0 || len(c.Name) > 120 {
		return fmt.Errorf("connection ID or name is invalid")
	}
	if c.Provider != "postgres" && c.Provider != "mysql" && c.Provider != "mongodb" && c.Provider != "supabase" {
		return fmt.Errorf("unsupported database provider")
	}
	if c.Host == "" || len(c.Host) > 253 || strings.ContainsAny(c.Host, " /\\@?#;\r\n\t") || strings.Contains(c.Host, "${") {
		return fmt.Errorf("use a hostname or IP address, not a connection URL")
	}
	if net.ParseIP(c.Host) == nil && !regexp.MustCompile(`^[a-zA-Z0-9.-]+$`).MatchString(c.Host) {
		return fmt.Errorf("invalid database hostname")
	}
	if c.Port < 1 || c.Port > 65535 || c.Database == "" || len(c.Database) > 128 || c.Username == "" || len(c.Username) > 128 || len(c.Password) > 8192 || strings.ContainsAny(c.Database+c.Username+c.Password, "\x00") {
		return fmt.Errorf("invalid database, username, password or port")
	}
	if c.Access != "read" && c.Access != "write" && c.Access != "schema" {
		return fmt.Errorf("access must be read, write or schema")
	}
	if c.SRV && (c.Provider != "mongodb" || !c.TLS) {
		return fmt.Errorf("SRV requires MongoDB with TLS")
	}
	return nil
}

func Validate(o *Operation) error {
	if o == nil || !ID.MatchString(o.ConnectionID) {
		return fmt.Errorf("select a saved database connection")
	}
	switch o.Operation {
	case "select", "insert", "update", "delete", "sql", "aggregate":
	default:
		return fmt.Errorf("unsupported database operation")
	}
	if o.Limit < 0 || o.Limit > 1000 {
		return fmt.Errorf("row limit must be 1 to 1000")
	}
	if o.Operation == "sql" {
		if strings.Contains(o.Query, ";") {
			return fmt.Errorf("run one SQL statement per node; omit semicolons")
		}
		if strings.TrimSpace(o.Query) == "" || len(o.Query) > 32768 || strings.Contains(o.Query, "${") {
			return fmt.Errorf("SQL is required; bind variables through parameters, not query text")
		}
	} else {
		if !identifier.MatchString(o.Table) || (o.Schema != "" && !identifier.MatchString(o.Schema)) {
			return fmt.Errorf("select a table/collection with a supported identifier")
		}
		if o.Query != "" || len(o.Parameters) > 0 {
			return fmt.Errorf("SQL fields require the SQL operation")
		}
	}
	if (o.Operation == "insert" || o.Operation == "update") && len(o.Values) == 0 {
		return fmt.Errorf("map at least one field")
	}
	if (o.Operation == "update" || o.Operation == "delete") && len(o.Filter) == 0 {
		return fmt.Errorf("update and delete require a filter")
	}
	if o.Operation == "aggregate" && len(o.Pipeline) == 0 {
		return fmt.Errorf("provide an aggregation pipeline")
	}
	for k := range o.Values {
		if !identifier.MatchString(k) {
			return fmt.Errorf("invalid mapped field name")
		}
	}
	for k := range o.Filter {
		if !identifier.MatchString(k) {
			return fmt.Errorf("filters use field names with equality values")
		}
	}
	for _, stage := range o.Pipeline {
		if !safePipeline(stage) {
			return fmt.Errorf("server-side code and write aggregation stages are not supported")
		}
		if len(stage) != 1 {
			return fmt.Errorf("each pipeline stage needs one operator")
		}
		for k := range stage {
			switch k {
			case "$match", "$project", "$group", "$sort", "$limit", "$skip", "$unwind", "$count", "$addFields", "$set", "$unset", "$replaceRoot", "$replaceWith":
			default:
				return fmt.Errorf("unsupported or write-capable aggregation stage")
			}
		}
	}
	if o.Operation != "aggregate" && len(o.Pipeline) > 0 {
		return fmt.Errorf("pipeline requires the aggregate operation")
	}
	if o.Operation == "sql" && (len(o.Values) > 0 || len(o.Filter) > 0) {
		return fmt.Errorf("use parameters for SQL, not mapped values or filters")
	}
	b, err := json.Marshal(o)
	if err != nil || len(b) > 65536 {
		return fmt.Errorf("database node configuration exceeds 64 KiB")
	}
	return nil
}
