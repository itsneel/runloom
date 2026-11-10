package dbnode

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// Private destinations require an operator-approved exact host:port. The same
// dial policy applies to discovery, tests and Mongo replica-set members.
type Network struct {
	Allowed []string
	Blocked []string
}

func (n Network) explicit(address string) bool {
	for _, blocked := range n.Blocked {
		if strings.EqualFold(address, strings.TrimSpace(blocked)) {
			return false
		}
	}
	for _, allowed := range n.Allowed {
		if strings.EqualFold(address, strings.TrimSpace(allowed)) {
			return true
		}
	}
	return false
}
func (n Network) Check(host string, port int, tls bool) error {
	address := net.JoinHostPort(host, fmt.Sprint(port))
	for _, blocked := range n.Blocked {
		if strings.EqualFold(address, strings.TrimSpace(blocked)) {
			return fmt.Errorf("Runloom infrastructure databases cannot be used as connections")
		}
	}
	if !tls && !n.explicit(address) {
		return fmt.Errorf("TLS is required unless an operator explicitly allows this host and port")
	}
	return nil
}
func (n Network) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("only TCP databases are supported")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid database address")
	}
	for _, blocked := range n.Blocked {
		if strings.EqualFold(address, strings.TrimSpace(blocked)) {
			return nil, &connectionPolicyError{"Database destination is blocked. Runloom infrastructure databases cannot be used as workflow connections."}
		}
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, &connectionPolicyError{"Database hostname cannot be resolved. Check Host and DNS access from the Runloom API and worker."}
	}
	// A second DNS alias must not bypass the application-storage exclusion.
	for _, blocked := range n.Blocked {
		blockedHost, blockedPort, splitErr := net.SplitHostPort(blocked)
		if splitErr != nil || blockedPort != port {
			continue
		}
		blockedIPs, resolveErr := net.DefaultResolver.LookupIPAddr(ctx, blockedHost)
		if resolveErr != nil {
			return nil, &connectionPolicyError{"Infrastructure destination policy could not be verified. Ask an operator to check cluster DNS; the connection was blocked for safety."}
		}
		for _, destination := range ips {
			for _, infrastructure := range blockedIPs {
				if destination.IP.Equal(infrastructure.IP) {
					return nil, &connectionPolicyError{"Database destination is blocked. Runloom infrastructure databases cannot be used as workflow connections."}
				}
			}
		}
	}
	for _, ip := range ips {
		if ip.IP.IsLinkLocalUnicast() || ip.IP.IsLinkLocalMulticast() || ip.IP.IsMulticast() || ip.IP.IsUnspecified() {
			return nil, &connectionPolicyError{"Database destination is blocked. Link-local and metadata addresses cannot be used as workflow connections."}
		}
		if !n.explicit(address) && (!ip.IP.IsGlobalUnicast() || ip.IP.IsPrivate() || ip.IP.IsLoopback() || net.ParseIP("100.64.0.0").Equal(ip.IP.Mask(net.CIDRMask(10, 32)))) {
			return nil, &connectionPolicyError{"Private database destination requires operator approval. Ask an operator to allow this exact host and port for the API and worker."}
		}
	}
	// Dial the validated address, not a second DNS lookup.
	return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ips[0].IP.String(), port))
}
