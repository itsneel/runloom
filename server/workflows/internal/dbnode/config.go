package dbnode

import (
	"github.com/example/runloom/workflows/internal/config"
	"net"
	"net/url"
	"strings"
)

func Configured() Executor {
	n := Network{Allowed: strings.Split(config.Value("RUNLOOM_DATABASE_NODE_ALLOWED_ADDRESSES"), ",")}
	if u, err := url.Parse(config.Value("RUNLOOM_DATABASE_URL")); err == nil && u.Host != "" {
		for _, host := range strings.Split(u.Host, ",") {
			if !strings.Contains(host, ":") {
				port := "5432"
				if strings.HasPrefix(u.Scheme, "mongo") {
					port = "27017"
				}
				host = net.JoinHostPort(host, port)
			}
			n.Blocked = append(n.Blocked, host)
		}
	}
	return Executor{Network: n}
}
