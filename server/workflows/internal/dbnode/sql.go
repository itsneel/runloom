package dbnode

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type Executor struct{ Network Network }

func (e Executor) openSQL(c Connection) (*sql.DB, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if err := e.Network.Check(c.Host, c.Port, c.TLS); err != nil {
		return nil, err
	}
	var db *sql.DB
	if c.Provider == "mysql" {
		cfg := mysql.NewConfig()
		cfg.User = c.Username
		cfg.Passwd = c.Password
		cfg.Net = "tcp"
		cfg.Addr = net.JoinHostPort(c.Host, fmt.Sprint(c.Port))
		cfg.DBName = c.Database
		cfg.Timeout = 5 * time.Second
		cfg.ReadTimeout = 25 * time.Second
		cfg.WriteTimeout = 25 * time.Second
		cfg.DialFunc = e.Network.DialContext
		if c.TLS {
			cfg.TLS = &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
		}
		connector, err := mysql.NewConnector(cfg)
		if err != nil {
			return nil, fmt.Errorf("invalid database connection")
		}
		db = sql.OpenDB(connector)
	} else {
		u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(c.Host, fmt.Sprint(c.Port)), Path: "/" + c.Database, User: url.UserPassword(c.Username, c.Password)}
		q := u.Query()
		q.Set("sslmode", "disable")
		if c.TLS {
			q.Set("sslmode", "verify-full")
		}
		q.Set("connect_timeout", "5")
		u.RawQuery = q.Encode()
		cfg, err := pgx.ParseConfig(u.String())
		if err != nil {
			return nil, fmt.Errorf("invalid database connection")
		}
		cfg.DialFunc = e.Network.DialContext
		cfg.LookupFunc = func(_ context.Context, host string) ([]string, error) { return []string{host}, nil }
		cfg.DefaultQueryExecMode = pgx.QueryExecModeExec
		cfg.RuntimeParams = map[string]string{"statement_timeout": "20000", "application_name": "runloom-database-node"}
		cfg.Fallbacks = nil
		db = stdlib.OpenDB(*cfg)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	db.SetConnMaxLifetime(time.Minute)
	return db, nil
}
func keys(m map[string]any) []string {
	k := make([]string, 0, len(m))
	for s := range m {
		k = append(k, s)
	}
	sort.Strings(k)
	return k
}
func sqlStatement(c Connection, o Operation) (string, []any, error) {
	if err := Validate(&o); err != nil {
		return "", nil, err
	}
	if o.Operation == "aggregate" {
		return "", nil, fmt.Errorf("aggregation is only available for MongoDB")
	}
	if o.Operation == "sql" {
		if strings.Contains(o.Query, ";") {
			return "", nil, fmt.Errorf("run one SQL statement per node; omit semicolons")
		}
		if c.Access != "schema" && !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(o.Query)), "SELECT ") {
			return "", nil, fmt.Errorf("raw writes and DDL require schema access; use mapped operations for writes")
		}
		return o.Query, o.Parameters, nil
	}
	quote := func(s string) string { return `"` + s + `"` }
	if c.Provider == "mysql" {
		quote = func(s string) string { return "`" + s + "`" }
	}
	table := quote(o.Table)
	if o.Schema != "" {
		table = quote(o.Schema) + "." + table
	}
	args := []any{}
	bind := func(v any) string {
		args = append(args, v)
		if c.Provider == "mysql" {
			return "?"
		}
		return fmt.Sprintf("$%d", len(args))
	}
	where := func() string {
		p := []string{}
		for _, k := range keys(o.Filter) {
			if o.Filter[k] == nil {
				p = append(p, quote(k)+" IS NULL")
			} else {
				p = append(p, quote(k)+" = "+bind(o.Filter[k]))
			}
		}
		if len(p) == 0 {
			return ""
		}
		return " WHERE " + strings.Join(p, " AND ")
	}
	var query string
	switch o.Operation {
	case "select":
		limit := o.Limit
		if limit == 0 {
			limit = 100
		}
		query = "SELECT * FROM " + table + where() + fmt.Sprintf(" LIMIT %d", limit+1)
	case "insert":
		cols, vals := []string{}, []string{}
		for _, k := range keys(o.Values) {
			cols = append(cols, quote(k))
			vals = append(vals, bind(o.Values[k]))
		}
		query = "INSERT INTO " + table + " (" + strings.Join(cols, ",") + ") VALUES (" + strings.Join(vals, ",") + ")"
	case "update":
		sets := []string{}
		for _, k := range keys(o.Values) {
			sets = append(sets, quote(k)+" = "+bind(o.Values[k]))
		}
		query = "UPDATE " + table + " SET " + strings.Join(sets, ",") + where()
	case "delete":
		query = "DELETE FROM " + table + where()
	}
	return query, args, nil
}
func readRows(rows *sql.Rows, limit int) (Result, error) {
	defer rows.Close()
	result := Result{Rows: []map[string]any{}}
	cols, err := rows.Columns()
	if err != nil {
		return result, err
	}
	for rows.Next() {
		if len(result.Rows) >= limit {
			result.Truncated = true
			break
		}
		values := make([]any, len(cols))
		p := make([]any, len(cols))
		for i := range values {
			p[i] = &values[i]
		}
		if err = rows.Scan(p...); err != nil {
			return result, err
		}
		row := map[string]any{}
		for i, k := range cols {
			switch v := values[i].(type) {
			case []byte:
				row[k] = string(v)
			case int64:
				row[k] = fmt.Sprint(v)
			default:
				row[k] = v
			}
		}
		result.Rows = append(result.Rows, row)
		b, err := json.Marshal(result)
		if err != nil || len(b) > 256*1024 {
			return Result{}, fmt.Errorf("database result exceeds 256 KiB; select fewer rows or columns")
		}
	}
	return result, rows.Err()
}
func (e Executor) sqlExecute(ctx context.Context, c Connection, o Operation) (Result, error) {
	query, args, err := sqlStatement(c, o)
	if err != nil {
		return Result{}, err
	}
	if c.Access == "read" && o.Operation != "select" && o.Operation != "sql" {
		return Result{}, fmt.Errorf("connection is read-only")
	}
	db, err := e.openSQL(c)
	if err != nil {
		return Result{}, err
	}
	defer db.Close()
	// Raw SQL outside schema mode is always in a server-enforced read-only transaction.
	readOnly := o.Operation == "select" || (o.Operation == "sql" && c.Access != "schema")
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: readOnly})
	if err != nil {
		return Result{}, fmt.Errorf("database connection or transaction failed")
	}
	defer tx.Rollback()
	result := Result{Rows: []map[string]any{}}
	if o.Operation == "select" || o.Operation == "sql" {
		rows, e := tx.QueryContext(ctx, query, args...)
		if e != nil {
			return result, fmt.Errorf("database query failed; verify syntax, parameters and database permissions")
		}
		limit := o.Limit
		if limit == 0 {
			limit = 100
		}
		result, err = readRows(rows, limit)
	} else {
		var r sql.Result
		r, err = tx.ExecContext(ctx, query, args...)
		if err == nil {
			result.Affected, err = r.RowsAffected()
		}
	}
	if err != nil {
		return Result{}, fmt.Errorf("database operation failed or result exceeded limits; a write may already have applied")
	}
	if err = tx.Commit(); err != nil {
		return Result{}, fmt.Errorf("database commit was not confirmed; verify the database before retrying")
	}
	return result, nil
}

func (e Executor) sqlDiscover(ctx context.Context, c Connection) ([]Table, error) {
	db, err := e.openSQL(c)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	query := `SELECT table_schema, table_name, column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema NOT IN ('pg_catalog','information_schema') ORDER BY table_schema,table_name,ordinal_position LIMIT 2001`
	args := []any{}
	if c.Provider == "mysql" {
		query = `SELECT table_schema, table_name, column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema = ? ORDER BY table_schema,table_name,ordinal_position LIMIT 2001`
		args = append(args, c.Database)
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, connectionError(err, c)
	}
	defer rows.Close()
	tables := []Table{}
	count := 0
	for rows.Next() {
		var schema, table, name, typ, nullable string
		if err = rows.Scan(&schema, &table, &name, &typ, &nullable); err != nil {
			return nil, fmt.Errorf("schema discovery failed")
		}
		count++
		if count > 2000 {
			return nil, fmt.Errorf("schema exceeds 2000 columns; use a database role limited to relevant tables")
		}
		if !identifier.MatchString(schema) || !identifier.MatchString(table) || !identifier.MatchString(name) {
			continue
		}
		if len(tables) == 0 || tables[len(tables)-1].Name != table || tables[len(tables)-1].Schema != schema {
			tables = append(tables, Table{Schema: schema, Name: table, Columns: []Column{}})
		}
		i := len(tables) - 1
		tables[i].Columns = append(tables[i].Columns, Column{Name: name, Type: typ, Nullable: nullable == "YES"})
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("schema discovery interrupted")
	}
	return tables, nil
}
