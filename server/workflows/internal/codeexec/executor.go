// Package codeexec launches a fresh capability-restricted WASM runner process.
// It never evaluates user code inside an API or Temporal worker process.
package codeexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/example/runloom/workflows/internal/codecontract"
	"github.com/example/runloom/workflows/internal/config"
	"github.com/example/runloom/workflows/internal/model"
)

var ErrBusy = errors.New("JavaScript capacity is busy; try again shortly")
var ErrUnavailable = errors.New("JavaScript runner unavailable; build runloom-code or set RUNLOOM_CODE_RUNNER")

// Limits are per host process, shared by all tenant API instances. No queue of
// unbounded input payloads: at most two children globally and one per team.
var capacity = struct {
	sync.Mutex
	total int
	teams map[string]bool
}{teams: map[string]bool{}}

type Result struct {
	Output json.RawMessage `json:"output,omitempty"`
	Error  string          `json:"error,omitempty"`
}

func Execute(ctx context.Context, input model.CodeActivityInput) (any, error) {
	if err := codecontract.ValidateInput(input); err != nil {
		return nil, err
	}
	teamID := input.TeamID
	capacity.Lock()
	if capacity.total >= 2 || capacity.teams[input.TeamID] {
		capacity.Unlock()
		return nil, ErrBusy
	}
	capacity.total++
	capacity.teams[input.TeamID] = true
	capacity.Unlock()
	defer func() { capacity.Lock(); capacity.total--; delete(capacity.teams, teamID); capacity.Unlock() }()
	path := config.Value("RUNLOOM_CODE_RUNNER")
	if path == "" {
		executable, err := os.Executable()
		if err != nil {
			return nil, ErrUnavailable
		}
		path = filepath.Join(filepath.Dir(executable), "runloom-code")
	}
	// The deadline includes process startup and all guest execution / serialization.
	ctx, cancel := context.WithTimeout(ctx, (codecontract.TimeoutSeconds+1)*time.Second)
	defer cancel()
	input.TeamID = "" // no tenant identity, credentials or host environment in the child.
	body, _ := json.Marshal(input)
	cmd := exec.CommandContext(ctx, path)
	cmd.Env = []string{"GOMAXPROCS=1", "GOMEMLIMIT=128MiB"}
	cmd.Stdin = bytes.NewReader(body)
	var stdout limitedBuffer
	cmd.Stdout = &stdout
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		return nil, ErrUnavailable
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("JavaScript execution canceled or exceeded its time limit")
		}
		return nil, fmt.Errorf("JavaScript runner failed or exceeded its resource limits")
	}
	if stdout.overflow {
		return nil, fmt.Errorf("JavaScript output exceeds 256 KiB")
	}
	var result Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("invalid JavaScript runner response")
	}
	if result.Error != "" {
		return nil, errors.New(result.Error)
	}
	if len(result.Output) > codecontract.MaxJSONBytes {
		return nil, fmt.Errorf("JavaScript output exceeds 256 KiB")
	}
	var output any
	if err := json.Unmarshal(result.Output, &output); err != nil {
		return nil, fmt.Errorf("JavaScript must return a JSON value")
	}
	return output, nil
}

type limitedBuffer struct {
	buffer   bytes.Buffer
	overflow bool
}

func (b *limitedBuffer) Bytes() []byte { return b.buffer.Bytes() }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > codecontract.MaxJSONBytes+8192 {
		b.overflow = true
		return 0, errors.New("output limit exceeded")
	}
	return b.buffer.Write(p)
}
