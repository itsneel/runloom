package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/runloom/workflows/internal/access"
	"github.com/example/runloom/workflows/internal/consumer"
	"github.com/example/runloom/workflows/internal/model"
	"github.com/example/runloom/workflows/internal/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	namespacepb "go.temporal.io/api/namespace/v1"
	"go.temporal.io/api/serviceerror"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
)

type machineVerifier map[string]consumer.Identity

func (v machineVerifier) Verify(_ context.Context, token string) (consumer.Identity, error) {
	a, ok := v[token]
	if !ok {
		return a, errors.New("invalid token")
	}
	return a, nil
}

type fakeNamespace struct {
	workflowservice.WorkflowServiceClient
}

func (f fakeNamespace) DescribeNamespace(context.Context, *workflowservice.DescribeNamespaceRequest, ...grpc.CallOption) (*workflowservice.DescribeNamespaceResponse, error) {
	return &workflowservice.DescribeNamespaceResponse{Config: &namespacepb.NamespaceConfig{WorkflowExecutionRetentionTtl: durationpb.New(24 * time.Hour)}}, nil
}

type executionClient struct {
	client.Client
	mu           sync.Mutex
	runs         map[string]enumspb.WorkflowExecutionStatus
	starts       int
	lostResponse bool
	options      client.StartWorkflowOptions
}
type executionValue struct{ payload *commonpb.Payload }

func (v executionValue) HasValue() bool { return v.payload != nil }
func (v executionValue) Get(value any) error {
	return converter.GetDefaultDataConverter().FromPayload(v.payload, value)
}

func (c *executionClient) WorkflowService() workflowservice.WorkflowServiceClient {
	return fakeNamespace{}
}
func (c *executionClient) ExecuteWorkflow(_ context.Context, o client.StartWorkflowOptions, _ any, _ ...any) (client.WorkflowRun, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.options = o
	if _, ok := c.runs[o.ID]; ok {
		return nil, &serviceerror.WorkflowExecutionAlreadyStarted{RunId: "22222222-2222-4222-8222-222222222222"}
	}
	c.runs[o.ID] = enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING
	c.starts++
	if c.lostResponse {
		c.lostResponse = false
		return nil, errors.New("simulated lost response with secret material")
	}
	return documentedRun{id: o.ID}, nil
}
func (c *executionClient) DescribeWorkflowExecution(_ context.Context, id, runID string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	status, ok := c.runs[id]
	if !ok {
		return nil, &serviceerror.NotFound{}
	}
	return &workflowservice.DescribeWorkflowExecutionResponse{WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{Execution: &commonpb.WorkflowExecution{WorkflowId: id, RunId: "22222222-2222-4222-8222-222222222222"}, Status: status}}, nil
}
func (c *executionClient) QueryWorkflow(context.Context, string, string, string, ...any) (converter.EncodedValue, error) {
	payload, err := converter.GetDefaultDataConverter().ToPayload(model.RunState{Output: map[string]any{"public": "ok", "private": "hidden", "details": map[string]any{"api_key": "never-expose", "safe": "visible"}}})
	if err != nil {
		return nil, err
	}
	return executionValue{payload}, nil
}
func (c *executionClient) CancelWorkflow(_ context.Context, id, _ string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.runs[id] = enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED
	return nil
}

type fakeProvider struct {
	secret     string
	generation string
	fail       bool
}

func (p *fakeProvider) Create(_ context.Context, id, generation, secret string, _ []string) (string, string, error) {
	p.secret, p.generation = secret, generation
	return "provider-" + id, "subject-" + id, nil
}
func (p *fakeProvider) Rotate(_ context.Context, id, generation, secret string, _ []string) error {
	p.secret, p.generation = secret, generation
	if p.fail {
		return errors.New("provider unavailable")
	}
	return nil
}
func (p *fakeProvider) Disable(context.Context, string) error {
	if p.fail {
		return errors.New("provider unavailable")
	}
	return nil
}

type consumerFixture struct {
	g         *Gateway
	c         *executionClient
	team, org string
	verifier  machineVerifier
	provider  *fakeProvider
	directory *access.Directory
	t         *testing.T
}

func newConsumerFixture(t *testing.T) *consumerFixture {
	return newConsumerFixtureClient(t, nil, "queue")
}

func TestConsumerRejectsAmbiguousAuthorization(t *testing.T) {
	f := newConsumerFixture(t)
	r := httptest.NewRequest("GET", "/api/v1/workflows", nil)
	r.Header.Add("Authorization", "Bearer first")
	r.Header.Add("Authorization", "Bearer second")
	w := httptest.NewRecorder()
	f.g.ServeHTTP(w, r)
	require.Equal(t, 401, w.Code)
}
func newConsumerFixtureClient(t *testing.T, temporalClient client.Client, queue string) *consumerFixture {
	ctx := context.Background()
	dir := t.TempDir()
	repo, err := access.Open(ctx, "file", "", "", dir)
	require.NoError(t, err)
	t.Cleanup(func() { repo.Close(ctx) })
	d := &access.Directory{Repo: repo}
	u := access.User{ID: "admin", Email: "admin@example.test"}
	_, err = d.Bind(ctx, u)
	require.NoError(t, err)
	org, err := d.CreateOrg(ctx, u, "Organization")
	require.NoError(t, err)
	state, _, err := repo.Read(ctx)
	require.NoError(t, err)
	team := ""
	for id := range state.Orgs[org].Teams {
		team = id
	}
	base, err := store.New(t.TempDir())
	require.NoError(t, err)
	c := &executionClient{runs: map[string]enumspb.WorkflowExecutionStatus{}}
	if temporalClient == nil {
		temporalClient = c
	}
	g := NewGateway(New(base, temporalClient, queue), d, testVerifier{"admin": u}, access.Config{}, func(ctx context.Context, id string) (store.Backend, error) {
		return store.OpenTeam(ctx, "file", "", "", dir, id)
	}, temporalClient, queue)
	t.Cleanup(g.Close)
	g.ConsumerStore, err = consumer.Open(ctx, "file", "", "", dir)
	require.NoError(t, err)
	t.Cleanup(func() { g.ConsumerStore.Close(ctx) })
	verifier := machineVerifier{}
	provider := &fakeProvider{}
	g.ConsumerVerifier, g.ConsumerProvider = verifier, provider
	tenant, err := g.tenant(ctx, team)
	require.NoError(t, err)
	for _, kind := range []string{"main", "child"} {
		_, err = tenant.backend.SaveDraft(model.Definition{SchemaVersion: "1.0", ID: kind, Name: kind, Kind: kind, InputSchema: map[string]any{"type": "object", "required": []any{"name"}}, Steps: []model.Step{{ID: "echo", Type: "activity", Activity: "runloom.echo"}}})
		require.NoError(t, err)
		_, err = tenant.backend.Publish(kind)
		require.NoError(t, err)
	}
	return &consumerFixture{g: g, c: c, team: team, org: org, verifier: verifier, provider: provider, directory: d, t: t}
}
func (f *consumerFixture) call(method, path, token, key string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
	r.Header.Set("Authorization", "Bearer "+token)
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	f.g.ServeHTTP(w, r)
	return w
}
func (f *consumerFixture) adminPath() string {
	return "/api/v1/studio/orgs/" + f.org + "/teams/" + f.team + "/service-accounts"
}
func (f *consumerFixture) account() consumer.Account {
	policy := consumer.Policy{Name: "Integration", Scopes: consumer.Scopes, RequestsPerMinute: 1000, MaxConcurrentRuns: 2, Grants: []consumer.Grant{{DefinitionID: "main", Versions: []int{1}, OutputFields: []string{"public", "details"}}}}
	w := f.call("POST", f.adminPath(), "admin", "", policy)
	require.Equal(f.t, 201, w.Code, w.Body.String())
	var response struct {
		Account consumer.Account `json:"account"`
		Secret  string           `json:"client_secret"`
	}
	require.NoError(f.t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Len(f.t, response.Secret, 43)
	a := response.Account
	f.verifier[a.ClientID] = consumer.Identity{ClientID: a.ClientID, Subject: a.Subject, Generation: a.Generation, Scopes: a.Scopes}
	return a
}
func TestServiceAccountScopesImmutable(t *testing.T) {
	f := newConsumerFixture(t)
	policy := consumer.Policy{Name: "Integration", Scopes: []string{"workflows:read", "runs:read"}, RequestsPerMinute: 60, MaxConcurrentRuns: 2, Grants: []consumer.Grant{{DefinitionID: "main", Versions: []int{1}}}}
	w := f.call("POST", f.adminPath(), "admin", "", policy)
	require.Equal(t, 201, w.Code, w.Body.String())
	var created struct {
		Account consumer.Account `json:"account"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	path := f.adminPath() + "/" + created.Account.ClientID
	before, _, err := f.directory.Repo.Read(context.Background())
	require.NoError(t, err)
	for _, scopes := range [][]string{
		{"workflows:read"},
		{"workflows:read", "runs:read", "runs:start"},
		{"workflows:read", "runs:start"},
		nil,
	} {
		changed := policy
		changed.Scopes = scopes
		changed.Name = "Must not persist"
		w = f.call("PUT", path, "admin", "", changed)
		require.Equal(t, 422, w.Code, w.Body.String())
		after, _, err := f.directory.Repo.Read(context.Background())
		require.NoError(t, err)
		require.Equal(t, before.ServiceAccounts, after.ServiceAccounts)
		require.Equal(t, before.Events, after.Events)
	}
	policy.Name = "Renamed integration"
	policy.RequestsPerMinute = 30
	policy.Scopes = []string{"runs:read", "workflows:read"}
	w = f.call("PUT", path, "admin", "", policy)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "client_secret")
	after, _, err := f.directory.Repo.Read(context.Background())
	require.NoError(t, err)
	saved := after.ServiceAccounts[created.Account.ClientID]
	require.Equal(t, policy.Name, saved.Name)
	require.Equal(t, 30, saved.RequestsPerMinute)
	require.Equal(t, created.Account.Scopes, saved.Scopes)
	require.Equal(t, "service_account.policy_updated", after.Events[len(after.Events)-1].Action)
}

func startBody() map[string]any {
	return map[string]any{"definition_id": "main", "version": 1, "input": map[string]any{"name": "Avery", "password": "hidden"}}
}
func responseID(t *testing.T, w *httptest.ResponseRecorder) string {
	var v struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &v))
	require.NotEmpty(t, v.ID)
	return v.ID
}

func TestConsumerAuthorizationRedactionAndSecrets(t *testing.T) {
	f := newConsumerFixture(t)
	a := f.account()
	b := f.account()
	listing := f.call("GET", f.adminPath(), "admin", "", nil)
	require.NotContains(t, listing.Body.String(), f.provider.secret)
	require.NotContains(t, listing.Body.String(), "client_secret")
	for _, path := range []string{"/api/v1/studio/me", "/api/v1/studio/definitions", "/api/v1/studio/orgs/private/llm-analytics", "/api/v1/studio/runs/private/llm-usage", "/api/v1/studio/definitions/child/test", "/api/v1/studio/code/test", f.adminPath()} {
		w := f.call("POST", path, a.ClientID, "", map[string]any{})
		require.Equal(t, 401, w.Code, path)
	}
	require.Equal(t, 401, f.call("GET", "/api/v1/workflows", "admin", "", nil).Code)
	w := f.call("GET", "/api/v1/workflows", a.ClientID, "", nil)
	require.Equal(t, 200, w.Code)
	require.NotContains(t, w.Body.String(), "steps")
	require.NotContains(t, w.Body.String(), "child")
	require.Equal(t, 422, f.call("POST", "/api/v1/runs", a.ClientID, "", startBody()).Code)
	invalid := startBody()
	invalid["input"] = map[string]any{}
	require.Equal(t, 422, f.call("POST", "/api/v1/runs", a.ClientID, "invalid-input", invalid).Code)
	w = f.call("POST", "/api/v1/runs", a.ClientID, "one-start", startBody())
	require.Equal(t, 202, w.Code, w.Body.String())
	id := responseID(t, w)
	require.Equal(t, 404, f.call("GET", "/api/v1/runs/"+id, b.ClientID, "", nil).Code)
	require.Equal(t, 404, f.call("POST", "/api/v1/runs/"+id+"/cancel", b.ClientID, "", nil).Code)
	require.NotContains(t, f.call("GET", "/api/v1/runs", b.ClientID, "", nil).Body.String(), id)
	w = f.call("GET", "/api/v1/runs/"+id, a.ClientID, "", nil)
	require.Equal(t, 200, w.Code)
	require.NotContains(t, w.Body.String(), "hidden")
	require.NotContains(t, w.Body.String(), "never-expose")
	require.NotContains(t, w.Body.String(), `"input"`)
	require.Contains(t, w.Body.String(), "[REDACTED]")
	require.Contains(t, w.Body.String(), "visible")
	state, _, err := f.directory.Repo.Read(context.Background())
	require.NoError(t, err)
	encoded, _ := json.Marshal(state)
	require.NotContains(t, string(encoded), f.provider.secret)
	old := f.verifier[a.ClientID]
	w = f.call("POST", f.adminPath()+"/"+a.ClientID+"/rotate", "admin", "", map[string]any{})
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 401, f.call("GET", "/api/v1/runs", a.ClientID, "", nil).Code)
	old.Generation = f.provider.generation
	f.verifier[a.ClientID] = old
	require.Equal(t, 200, f.call("GET", "/api/v1/runs", a.ClientID, "", nil).Code)
	f.provider.fail = true
	w = f.call("POST", f.adminPath()+"/"+a.ClientID+"/revoke", "admin", "", map[string]any{})
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"provider_disabled":false`)
	require.Equal(t, 401, f.call("GET", "/api/v1/runs", a.ClientID, "", nil).Code)
}
func TestConsumerIdempotencyConcurrencyAndQuotas(t *testing.T) {
	f := newConsumerFixture(t)
	a := f.account()
	var wg sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			responses <- f.call("POST", "/api/v1/runs", a.ClientID, "parallel-key", startBody())
		}()
	}
	wg.Wait()
	close(responses)
	id := ""
	for w := range responses {
		require.Equal(t, 202, w.Code, w.Body.String())
		got := responseID(t, w)
		if id == "" {
			id = got
		}
		require.Equal(t, id, got)
	}
	require.Equal(t, 1, f.c.starts)
	require.Equal(t, enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE, f.c.options.WorkflowIDReusePolicy)
	require.True(t, f.c.options.WorkflowExecutionErrorWhenAlreadyStarted)
	changed := startBody()
	changed["input"] = map[string]any{"name": "Different"}
	require.Equal(t, 409, f.call("POST", "/api/v1/runs", a.ClientID, "parallel-key", changed).Code)
	require.Equal(t, 202, f.call("POST", "/api/v1/runs", a.ClientID, "second-key", startBody()).Code)
	require.Equal(t, 429, f.call("POST", "/api/v1/runs", a.ClientID, "third-key", startBody()).Code)
	require.Equal(t, 202, f.call("POST", "/api/v1/runs/"+id+"/cancel", a.ClientID, "", nil).Code)
	require.Equal(t, 202, f.call("POST", "/api/v1/runs", a.ClientID, "third-key", startBody()).Code)
	starts := f.c.starts
	require.Equal(t, 202, f.call("POST", "/api/v1/runs", a.ClientID, "parallel-key", startBody()).Code)
	require.Equal(t, starts, f.c.starts)
	w := f.call("GET", "/api/v1/runs?limit=1", a.ClientID, "", nil)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"next_cursor":"1"`)
	require.NoError(t, f.directory.UpdateModule(context.Background(), "consumers", func(s *access.State) error {
		v := s.ServiceAccounts[a.ClientID]
		v.RequestsPerMinute = 1
		s.ServiceAccounts[a.ClientID] = v
		return nil
	}))
	require.Equal(t, 429, f.call("GET", "/api/v1/runs", a.ClientID, "", nil).Code)
}
func TestConsumerLostResponseAndStaleStart(t *testing.T) {
	f := newConsumerFixture(t)
	a := f.account()
	f.c.lostResponse = true
	w := f.call("POST", "/api/v1/runs", a.ClientID, "lost-key", startBody())
	require.Equal(t, 503, w.Code)
	require.NotContains(t, w.Body.String(), "secret material")
	w = f.call("POST", "/api/v1/runs", a.ClientID, "lost-key", startBody())
	require.Equal(t, 202, w.Code, w.Body.String())
	require.Equal(t, 1, f.c.starts)
	key := "stale-key"
	id := f.team + "-consumer-" + uuid.NewSHA1(uuid.NameSpaceOID, []byte(a.ClientID+":"+hashValue(key))).String()
	require.NoError(t, f.g.updateLedger(context.Background(), a.ClientID, func(l *consumer.Ledger) error {
		request := struct {
			DefinitionID string         `json:"definition_id"`
			Version      int            `json:"version"`
			Input        map[string]any `json:"input"`
		}{"main", 1, map[string]any{"name": "Avery", "password": "hidden"}}
		l.Runs[id] = consumer.Run{ID: id, Fingerprint: hashValue(request), DefinitionID: "main", Version: 1, Status: "pending", AttemptedAt: time.Now().Add(-time.Hour), Sequence: 2}
		return nil
	}))
	w = f.call("POST", "/api/v1/runs", a.ClientID, key, startBody())
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "start_uncertain")
	require.Equal(t, 1, f.c.starts)
}

func TestConsumerScopesGrantsAndAdminRoles(t *testing.T) {
	f := newConsumerFixture(t)
	a := f.account()
	ctx := context.Background()
	humanTokens := f.g.Verifier.(testVerifier)
	for _, role := range []string{"viewer", "editor"} {
		u := access.User{ID: role, Email: role + "@example.test"}
		humanTokens[role] = u
		_, err := f.directory.Bind(ctx, u)
		require.NoError(t, err)
		admin := humanTokens["admin"]
		require.NoError(t, f.directory.SetMember(ctx, admin, f.org, "", u.Email, "member", false))
		require.NoError(t, f.directory.SetMember(ctx, admin, f.org, f.team, u.Email, role, false))
		_, err = f.directory.Bind(ctx, u)
		require.NoError(t, err)
		require.Equal(t, 403, f.call("GET", f.adminPath(), role, "", nil).Code)
		require.Equal(t, 403, f.call("POST", f.adminPath()+"/"+a.ClientID+"/rotate", role, "", map[string]any{}).Code)
	}
	policy := a.Policy
	policy.Grants = []consumer.Grant{{DefinitionID: "child", Versions: []int{1}}}
	require.Equal(t, 422, f.call("POST", f.adminPath(), "admin", "", policy).Code)
	require.Equal(t, 403, f.call("POST", "/api/v1/runs", a.ClientID, "child-key", map[string]any{"definition_id": "child", "version": 1, "input": map[string]any{"name": "Avery"}}).Code)
	identity := f.verifier[a.ClientID]
	identity.Scopes = []string{"runs:read"}
	f.verifier[a.ClientID] = identity
	require.Equal(t, 403, f.call("POST", "/api/v1/runs", a.ClientID, "scope-key", startBody()).Code)
	identity.Scopes = a.Scopes
	f.verifier[a.ClientID] = identity
	w := f.call("POST", "/api/v1/runs", a.ClientID, "grant-key", startBody())
	require.Equal(t, 202, w.Code)
	id := responseID(t, w)
	tenant, err := f.g.tenant(ctx, f.team)
	require.NoError(t, err)
	events, err := tenant.backend.ListAudit(id, "", 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "service-account:"+a.ClientID, events[0].Actor)
	request := httptest.NewRequest("GET", "/api/v1/runs/"+id, nil)
	request.Header.Set("Authorization", "Bearer "+a.ClientID)
	request.Header.Set("X-Runloom-Team", uuid.NewString())
	response := httptest.NewRecorder()
	f.g.ServeHTTP(response, request)
	require.Equal(t, 403, response.Code)
	require.NoError(t, f.directory.UpdateModule(ctx, "consumers", func(s *access.State) error {
		v := s.ServiceAccounts[a.ClientID]
		v.Grants = nil
		s.ServiceAccounts[a.ClientID] = v
		return nil
	}))
	require.Equal(t, 404, f.call("GET", "/api/v1/runs/"+id, a.ClientID, "", nil).Code)
	require.Equal(t, 404, f.call("POST", "/api/v1/runs/"+id+"/cancel", a.ClientID, "", nil).Code)
}

func TestConsumerParallelDistinctKeysCannotExceedQuota(t *testing.T) {
	f := newConsumerFixture(t)
	a := f.account()
	var wg sync.WaitGroup
	responses := make(chan int, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			responses <- f.call("POST", "/api/v1/runs", a.ClientID, uuid.NewString(), startBody()).Code
		}()
	}
	wg.Wait()
	close(responses)
	accepted := 0
	for code := range responses {
		if code == 202 {
			accepted++
		} else {
			require.Equal(t, 429, code)
		}
	}
	require.Equal(t, 2, accepted)
	require.Equal(t, 2, f.c.starts)
}

type faultConsumerStore struct {
	consumer.Repository
	failed bool
}

func (f *faultConsumerStore) Swap(ctx context.Context, key string, rev int64, v any) (bool, error) {
	if l, ok := v.(consumer.Ledger); ok && !f.failed {
		for _, r := range l.Runs {
			if r.TemporalRunID != "" {
				f.failed = true
				return false, errors.New("simulated persistence failure after acceptance")
			}
		}
	}
	return f.Repository.Swap(ctx, key, rev, v)
}
func TestConsumerPersistenceFailureNeverCancelsOrRestarts(t *testing.T) {
	f := newConsumerFixture(t)
	a := f.account()
	underlying := f.g.ConsumerStore
	f.g.ConsumerStore = &faultConsumerStore{Repository: underlying}
	response := f.call("POST", "/api/v1/runs", a.ClientID, "durable-key", startBody())
	require.Equal(t, 503, response.Code)
	require.Equal(t, 1, f.c.starts)
	// A fresh handler/storage adapter recovers the same accepted execution.
	f.g.ConsumerStore = underlying
	response = f.call("POST", "/api/v1/runs", a.ClientID, "durable-key", startBody())
	require.Equal(t, 202, response.Code, response.Body.String())
	require.Equal(t, 1, f.c.starts)
	id := responseID(t, response)
	require.Equal(t, enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING, f.c.runs[id])
	f.c.mu.Lock()
	f.c.runs[id] = enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED
	f.c.mu.Unlock()
	require.Equal(t, 200, f.call("GET", "/api/v1/runs/"+id, a.ClientID, "", nil).Code)
	f.c.mu.Lock()
	delete(f.c.runs, id) // Temporal retention expires; durable mapping must still win.
	f.c.mu.Unlock()
	response = f.call("POST", "/api/v1/runs", a.ClientID, "durable-key", startBody())
	require.Equal(t, 202, response.Code, response.Body.String())
	require.Equal(t, 1, f.c.starts)
}

func TestConsumerOpenAPI(t *testing.T) {
	var spec map[string]any
	require.NoError(t, json.Unmarshal(consumerOpenAPI, &spec))
	require.Equal(t, "3.0.3", spec["openapi"])
	paths := spec["paths"].(map[string]any)
	require.Len(t, paths, 5)
	for _, path := range []string{"/workflows", "/workflows/{id}", "/runs", "/runs/{id}", "/runs/{id}/cancel"} {
		require.Contains(t, paths, path)
	}
	f := newConsumerFixture(t)
	response := f.call("GET", "/api/v1/openapi.json", "", "", nil)
	require.Equal(t, 200, response.Code)
	require.JSONEq(t, string(consumerOpenAPI), response.Body.String())
}
