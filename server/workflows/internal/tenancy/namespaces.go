// Package tenancy owns Temporal namespace provisioning and client lifecycle.
// Namespace identity is deterministic and independent of the application DB driver.
package tenancy

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/example/runloom/workflows/internal/config"
	"github.com/example/runloom/workflows/internal/protection"
	"github.com/google/uuid"
	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/operatorservice/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"google.golang.org/protobuf/types/known/durationpb"
)

type Pool struct {
	Cluster         client.Client
	prefix, address string
	retention       time.Duration
	mu              sync.Mutex
	clients         map[string]client.Client
	closed          bool
}

func New(cluster client.Client, cfg config.Settings) (*Pool, error) {
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`).MatchString(cfg.TemporalNamespacePrefix) || cfg.TemporalRetentionDays < 1 || cfg.TemporalRetentionDays > 3650 {
		return nil, fmt.Errorf("invalid Temporal namespace prefix or retention (1..3650 days)")
	}
	return &Pool{Cluster: cluster, prefix: cfg.TemporalNamespacePrefix, address: cfg.TemporalAddress, retention: time.Duration(cfg.TemporalRetentionDays) * 24 * time.Hour, clients: map[string]client.Client{}}, nil
}

func (p *Pool) Name(org string) (string, error) {
	id, err := uuid.Parse(org)
	if err != nil || id.String() != org {
		return "", fmt.Errorf("invalid organization identity")
	}
	return p.prefix + "-" + org, nil
}

func Attributes() map[string]enums.IndexedValueType {
	a := map[string]enums.IndexedValueType{}
	for _, name := range []string{"RunloomOrgId", "RunloomTeamId", "RunloomProjectId", "RunloomDefinitionId", "RunloomSource", "RunloomRootExecutionId", "RunloomSnapshotId"} {
		a[name] = enums.INDEXED_VALUE_TYPE_KEYWORD
	}
	a["RunloomDefinitionVersion"] = enums.INDEXED_VALUE_TYPE_INT
	a["RunloomIsTest"] = enums.INDEXED_VALUE_TYPE_BOOL
	return a
}

// Ensure is safe across replicas. Existing namespaces must bear the correct
// ownership marker; a name collision or incompatible attribute type fails closed.
func (p *Pool) Ensure(ctx context.Context, org string) (client.Client, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	name, err := p.Name(org)
	if err != nil {
		return nil, "", err
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, "", fmt.Errorf("namespace pool closed")
	}
	if c := p.clients[name]; c != nil {
		p.mu.Unlock()
		return c, name, nil
	}
	p.mu.Unlock()
	desc, err := p.Cluster.WorkflowService().DescribeNamespace(ctx, &workflowservice.DescribeNamespaceRequest{Namespace: name})
	var missing *serviceerror.NamespaceNotFound
	if errors.As(err, &missing) {
		_, err = p.Cluster.WorkflowService().RegisterNamespace(ctx, &workflowservice.RegisterNamespaceRequest{Namespace: name, WorkflowExecutionRetentionPeriod: durationpb.New(p.retention), Data: map[string]string{"runloom_org_id": org, "runloom_prefix": p.prefix}})
		var exists *serviceerror.NamespaceAlreadyExists
		if err != nil && !errors.As(err, &exists) {
			return nil, "", err
		}
		desc, err = p.Cluster.WorkflowService().DescribeNamespace(ctx, &workflowservice.DescribeNamespaceRequest{Namespace: name})
	}
	if err != nil {
		return nil, "", err
	}
	info := desc.GetNamespaceInfo()
	if info.GetData()["runloom_org_id"] != org || info.GetData()["runloom_prefix"] != p.prefix || info.GetState() != enums.NAMESPACE_STATE_REGISTERED {
		return nil, "", fmt.Errorf("namespace ownership or state mismatch")
	}
	if err = p.ensureAttributes(ctx, name); err != nil {
		return nil, "", err
	}
	options, err := protection.TemporalOptions(p.address, name)
	if err != nil {
		return nil, "", err
	}
	c, err := client.NewClientFromExisting(p.Cluster, options)
	if err != nil {
		return nil, "", err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		c.Close()
		return nil, "", fmt.Errorf("namespace pool closed")
	}
	if existing := p.clients[name]; existing != nil {
		c.Close()
		return existing, name, nil
	}
	p.clients[name] = c
	return c, name, nil
}

func (p *Pool) ensureAttributes(ctx context.Context, name string) error {
	// Retry namespace-cache propagation and concurrent registrations, bounded by
	// the caller deadline. Never start an untagged run when provisioning fails.
	for {
		listed, err := p.Cluster.OperatorService().ListSearchAttributes(ctx, &operatorservice.ListSearchAttributesRequest{Namespace: name})
		if err == nil {
			missing := map[string]enums.IndexedValueType{}
			for k, v := range Attributes() {
				actual, ok := listed.CustomAttributes[k]
				if ok && actual != v {
					return fmt.Errorf("search attribute %s has incompatible type", k)
				}
				if !ok {
					missing[k] = v
				}
			}
			if len(missing) == 0 {
				return nil
			}
			_, err = p.Cluster.OperatorService().AddSearchAttributes(ctx, &operatorservice.AddSearchAttributesRequest{Namespace: name, SearchAttributes: missing})
			if err != nil {
				var exists *serviceerror.AlreadyExists
				if !errors.As(err, &exists) {
					return err
				}
			}
		} else {
			var missing *serviceerror.NamespaceNotFound
			if !errors.As(err, &missing) {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// List returns only this deployment's provisioned org namespaces. Workers do
// not receive the Auth directory or credentials to read human identities.
func (p *Pool) List(ctx context.Context) ([]string, error) {
	var out []string
	var token []byte
	for {
		r, err := p.Cluster.WorkflowService().ListNamespaces(ctx, &workflowservice.ListNamespacesRequest{PageSize: 100, NextPageToken: token})
		if err != nil {
			return nil, err
		}
		for _, d := range r.Namespaces {
			i := d.GetNamespaceInfo()
			name, err := p.Name(i.GetData()["runloom_org_id"])
			if err == nil && i.GetName() == name && i.GetData()["runloom_prefix"] == p.prefix && i.GetState() == enums.NAMESPACE_STATE_REGISTERED {
				// Only expose namespaces after their full attribute schema is ready.
				a, err := p.Cluster.OperatorService().ListSearchAttributes(ctx, &operatorservice.ListSearchAttributesRequest{Namespace: name})
				if err != nil {
					return nil, err
				}
				ready := true
				for k, v := range Attributes() {
					if a.CustomAttributes[k] != v {
						ready = false
					}
				}
				if ready {
					out = append(out, name)
				}
			}
		}
		token = r.NextPageToken
		if len(token) == 0 {
			break
		}
	}
	sort.Strings(out)
	return out, nil
}

func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	for _, c := range p.clients {
		c.Close()
	}
	p.clients = map[string]client.Client{}
}
