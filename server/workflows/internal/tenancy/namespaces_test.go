package tenancy

import (
	"context"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/config"
	"github.com/stretchr/testify/require"
	enums "go.temporal.io/api/enums/v1"
	namespacepb "go.temporal.io/api/namespace/v1"
	"go.temporal.io/api/operatorservice/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc"
)

type testClient struct {
	client.Client
	workflow workflowservice.WorkflowServiceClient
	operator operatorservice.OperatorServiceClient
}

func (c testClient) WorkflowService() workflowservice.WorkflowServiceClient { return c.workflow }
func (c testClient) OperatorService() operatorservice.OperatorServiceClient { return c.operator }

type namespaceService struct {
	workflowservice.WorkflowServiceClient
	info *namespacepb.NamespaceInfo
}

func (s namespaceService) DescribeNamespace(context.Context, *workflowservice.DescribeNamespaceRequest, ...grpc.CallOption) (*workflowservice.DescribeNamespaceResponse, error) {
	return &workflowservice.DescribeNamespaceResponse{NamespaceInfo: s.info}, nil
}

type attributesService struct {
	operatorservice.OperatorServiceClient
	values map[string]enums.IndexedValueType
	calls  int
	denied bool
	race   bool
}

func (s *attributesService) ListSearchAttributes(context.Context, *operatorservice.ListSearchAttributesRequest, ...grpc.CallOption) (*operatorservice.ListSearchAttributesResponse, error) {
	return &operatorservice.ListSearchAttributesResponse{CustomAttributes: s.values}, nil
}
func (s *attributesService) AddSearchAttributes(_ context.Context, r *operatorservice.AddSearchAttributesRequest, _ ...grpc.CallOption) (*operatorservice.AddSearchAttributesResponse, error) {
	s.calls++
	if s.denied {
		return nil, serviceerror.NewPermissionDenied("denied", "")
	}
	for k, v := range r.SearchAttributes {
		s.values[k] = v
	}
	if s.race {
		return nil, serviceerror.NewAlreadyExists("concurrent replica")
	}
	return &operatorservice.AddSearchAttributesResponse{}, nil
}

func TestNamespaceIdentityAndConfig(t *testing.T) {
	cfg := config.Settings{TemporalNamespacePrefix: "runloom-test", TemporalRetentionDays: 30}
	p, err := New(nil, cfg)
	require.NoError(t, err)
	name, err := p.Name("00000000-0000-4000-8000-000000000001")
	require.NoError(t, err)
	require.Equal(t, "runloom-test-00000000-0000-4000-8000-000000000001", name)
	for _, org := range []string{"", "my organization", "00000000-0000-4000-8000-00000000000A"} {
		_, err = p.Name(org)
		require.Error(t, err)
	}
	for _, prefix := range []string{"", "prod/Test", "contains spaces"} {
		cfg.TemporalNamespacePrefix = prefix
		_, err = New(nil, cfg)
		require.Error(t, err)
	}
	cfg.TemporalNamespacePrefix = "valid"
	cfg.TemporalRetentionDays = 0
	_, err = New(nil, cfg)
	require.Error(t, err)
}

func TestNamespaceOwnershipFailsClosed(t *testing.T) {
	c := testClient{workflow: namespaceService{info: &namespacepb.NamespaceInfo{State: enums.NAMESPACE_STATE_REGISTERED, Data: map[string]string{"runloom_org_id": "another-org"}}}}
	p, err := New(c, config.Settings{TemporalNamespacePrefix: "test", TemporalRetentionDays: 30})
	require.NoError(t, err)
	_, _, err = p.Ensure(context.Background(), "00000000-0000-4000-8000-000000000001")
	require.ErrorContains(t, err, "ownership")
}

func TestAttributeProvisioningIdempotencyAndFailures(t *testing.T) {
	for _, race := range []bool{false, true} {
		s := &attributesService{values: map[string]enums.IndexedValueType{}, race: race}
		p := &Pool{Cluster: testClient{operator: s}}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		require.NoError(t, p.ensureAttributes(ctx, "test"))
		require.NoError(t, p.ensureAttributes(ctx, "test"))
		cancel()
		require.Equal(t, 1, s.calls)
		require.Equal(t, Attributes(), s.values)
	}
	s := &attributesService{values: map[string]enums.IndexedValueType{"RunloomOrgId": enums.INDEXED_VALUE_TYPE_TEXT}}
	p := &Pool{Cluster: testClient{operator: s}}
	require.ErrorContains(t, p.ensureAttributes(context.Background(), "test"), "incompatible type")
	require.Zero(t, s.calls)
	s.values = map[string]enums.IndexedValueType{}
	s.denied = true
	require.Error(t, p.ensureAttributes(context.Background(), "test"))
}
