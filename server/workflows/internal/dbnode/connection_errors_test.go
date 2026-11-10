package dbnode

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"syscall"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/description"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/topology"
)

// Read-only integration check: no saved Runloom connections or external records
// are created. Supply an isolated database user created in sample_db.
func TestLiveSampleConnectionDiagnostics(t *testing.T) {
	portText := os.Getenv("RUNLOOM_DIAGNOSTIC_MONGO_PORT")
	if portText == "" {
		t.Skip("optional local sample MongoDB diagnostic check")
	}
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)
	c := Connection{ID: "diagnostics", Name: "Diagnostics", Provider: "mongodb", Host: "127.0.0.1", Port: port, Database: "sample_db", AuthSource: "sample_db", Username: "sample_user", Password: os.Getenv("RUNLOOM_DIAGNOSTIC_MONGO_PASSWORD"), Enabled: true, Access: "read"}
	require.NotEmpty(t, c.Password)
	e := Executor{Network: Network{Allowed: []string{net.JoinHostPort(c.Host, portText)}}}
	require.NoError(t, e.Test(context.Background(), c))
	wrong := c
	wrong.AuthSource = ""
	err = e.Test(context.Background(), wrong)
	require.ErrorContains(t, err, "Authentication database is blank")
	require.NotContains(t, err.Error(), c.Password)
	wrong.AuthSource = "sample_db"
	wrong.Password = "synthetic-invalid-password"
	err = e.Test(context.Background(), wrong)
	require.ErrorContains(t, err, "Authentication database may be incorrect")
	require.NotContains(t, err.Error(), wrong.Password)
}

func TestConnectionErrorsExplainCausesWithoutDriverDetails(t *testing.T) {
	const secret = "synthetic-password-must-not-appear"
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"mongo auth", mongo.CommandError{Code: 18, Message: secret}, "Authentication database is blank"},
		{"mongo permissions", mongo.CommandError{Code: 13, Message: secret}, "MongoDB permission denied"},
		{"postgres auth", &pgconn.PgError{Code: "28P01", Message: secret}, "authentication failed"},
		{"postgres database", &pgconn.PgError{Code: "3D000", Message: secret}, "database does not exist"},
		{"postgres permission", &pgconn.PgError{Code: "42501", Message: secret}, "permission denied"},
		{"mysql auth", &mysql.MySQLError{Number: 1045, Message: secret}, "MySQL authentication failed"},
		{"mysql database", &mysql.MySQLError{Number: 1049, Message: secret}, "database does not exist"},
		{"mysql permission", &mysql.MySQLError{Number: 1142, Message: secret}, "permission denied"},
		{"dns", &net.DNSError{Err: secret, Name: secret}, "hostname could not be resolved"},
		{"refused", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, "connection refused"},
		{"timeout", context.DeadlineExceeded, "timed out"},
		{"tls certificate", x509.UnknownAuthorityError{}, "TLS certificate verification failed"},
		{"tls handshake", tls.RecordHeaderError{Msg: secret}, "TLS handshake failed"},
		{"unknown", errors.New(secret), "connection failed"},
		{"policy", &connectionPolicyError{"Private database destination requires operator approval."}, "operator approval"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := connectionError(fmt.Errorf("driver contains %s: %w", secret, tc.err), Connection{Provider: "mongodb", Password: secret})
			require.ErrorContains(t, err, tc.want)
			require.NotContains(t, err.Error(), secret)
		})
	}
}

func TestMongoMonitorCauseTakesPrecedenceOverSelectionTimeout(t *testing.T) {
	err := topology.ServerSelectionError{Wrapped: context.DeadlineExceeded, Desc: description.Topology{Servers: []description.Server{{LastError: mongo.CommandError{Code: 18, Message: "raw sensitive server text"}}}}}
	got := connectionError(err, Connection{Provider: "mongodb", AuthSource: "sample_db"})
	require.ErrorContains(t, got, "Authentication database may be incorrect")
	require.NotContains(t, got.Error(), "blank")
	require.NotContains(t, got.Error(), "raw sensitive")
}
