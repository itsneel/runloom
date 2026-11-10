package dbnode

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"syscall"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/auth"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/topology"
)

// Never expose driver error text: it can include credentials, connection URLs,
// queries or server-controlled content. Only return these fixed diagnoses.
type connectionPolicyError struct{ message string }

func (e *connectionPolicyError) Error() string { return e.message }

func connectionError(err error, c Connection) error {
	if diagnosis := connectionDiagnosis(err, c, 0); diagnosis != "" {
		return errors.New(diagnosis)
	}
	return errors.New("The database connection failed. Check the host, port and database server configuration, then try again.")
}

func connectionDiagnosis(err error, c Connection, depth int) string {
	if err == nil || depth > 8 {
		return ""
	}
	var policy *connectionPolicyError
	if errors.As(err, &policy) {
		return policy.message
	}
	var selection topology.ServerSelectionError
	if errors.As(err, &selection) {
		// MongoDB's monitor retains the concrete cause in the server description;
		// the top-level error often only wraps a server-selection timeout.
		for _, server := range selection.Desc.Servers {
			if diagnosis := connectionDiagnosis(server.LastError, c, depth+1); diagnosis != "" {
				return diagnosis
			}
		}
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "28P01", "28000":
			return "Database authentication failed. Check the username and password, and whether the server allows this user to connect from Runloom."
		case "3D000":
			return "The database does not exist. Check the Database field or create the database first."
		case "42501":
			return "Database permission denied. Grant this user access to the selected database and permission for the requested operation."
		case "53300":
			return "The database has too many connections. Try again later or increase the server connection limit."
		}
	}
	var my *mysql.MySQLError
	if errors.As(err, &my) {
		switch my.Number {
		case 1045:
			return "MySQL authentication failed. Check the username and password, and the user's allowed client hosts."
		case 1049:
			return "The database does not exist. Check the Database field or create the database first."
		case 1044, 1142, 1143, 1227:
			return "Database permission denied. Grant this user access to the selected database and permission for the requested operation."
		case 1040:
			return "The database has too many connections. Try again later or increase the server connection limit."
		}
	}
	var authentication *auth.Error
	var command mongo.ServerError
	if errors.As(err, &authentication) || (errors.As(err, &command) && command.HasErrorCode(18)) {
		if c.AuthSource == "" {
			return "MongoDB authentication failed. Authentication database is blank, so Runloom used admin. Set Authentication database to the database where this user was created (which may be your data database), and verify the username and password."
		}
		return "MongoDB authentication failed. Authentication database may be incorrect: use the database where this user was created, then verify the username and password. MongoDB does not distinguish these causes."
	}
	if errors.As(err, &command) && command.HasErrorCode(13) {
		return "MongoDB permission denied. Grant this user the required role on the selected database; schema discovery also needs collection-listing and read permissions."
	}
	var certificate *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	if errors.As(err, &certificate) || errors.As(err, &authority) || errors.As(err, &hostname) || errors.As(err, &invalid) {
		return "TLS certificate verification failed. Check that the certificate is trusted, unexpired and valid for the Host field. Do not disable TLS for a public database."
	}
	var tlsRecord tls.RecordHeaderError
	if errors.As(err, &tlsRecord) {
		return "TLS handshake failed. Check the port and whether the database supports TLS. Plaintext is permitted only for an operator-approved local or private endpoint."
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "Database hostname could not be resolved. Check Host and DNS access from the Runloom API and worker; localhost refers to their container, not your computer."
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "Database connection refused. Check the port and that the database is running and accepting connections from Runloom."
	}
	var network net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()) {
		return "Database connection timed out. Check firewall rules, routing and database availability from the Runloom API and worker."
	}
	return ""
}
