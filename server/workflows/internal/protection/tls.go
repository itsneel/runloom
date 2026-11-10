package protection

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/example/runloom/workflows/internal/config"
)

func temporalTLS() (*tls.Config, error) {
	if value := config.Value("TEMPORAL_TLS_ENABLED"); value != "" && value != "true" && value != "false" {
		return nil, fmt.Errorf("TEMPORAL_TLS_ENABLED must be true or false")
	}
	ca := config.Value("TEMPORAL_TLS_CA_FILE")
	cert := config.Value("TEMPORAL_TLS_CERT_FILE")
	key := config.Value("TEMPORAL_TLS_KEY_FILE")
	server := config.Value("TEMPORAL_TLS_SERVER_NAME")
	if ca == "" && cert == "" && key == "" && server == "" && config.Value("TEMPORAL_TLS_ENABLED") != "true" {
		return nil, nil
	}
	out := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: server}
	if ca != "" {
		raw, err := os.ReadFile(ca)
		if err != nil {
			return nil, fmt.Errorf("Temporal CA file unavailable")
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(raw) {
			return nil, fmt.Errorf("Temporal CA file invalid")
		}
		out.RootCAs = roots
	}
	if cert != "" || key != "" {
		identity, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			return nil, fmt.Errorf("Temporal client certificate/key unavailable or invalid")
		}
		out.Certificates = []tls.Certificate{identity}
	}
	return out, nil
}
