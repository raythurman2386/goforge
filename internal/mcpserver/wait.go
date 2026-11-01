package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Operation kinds, as the API's operation.kind names them.
const (
	kindDeployment = "deployment"
	kindService    = "service"
	statusFailed   = "failed"
)

const (
	// defaultWait and maxWait keep a wait_for call under the 15 second
	// timeout on /mcp, with room to read the log tail.
	defaultWait    = 10 * time.Second
	maxWait        = 12 * time.Second
	defaultLogTail = 20
	maxLogTail     = 200
	// logReadTimeout bounds the log tail read after the wait.
	logReadTimeout = 2 * time.Second
)

// waitPollInterval is how often wait_for reads the status. Tests shorten it.
var waitPollInterval = time.Second

type waitForInput struct {
	Kind           string `json:"kind" jsonschema:"deployment or service: the operation.kind a mutation returned"`
	ID             string `json:"id" jsonschema:"the deployment or service instance UUID: operation.id"`
	ProjectID      string `json:"project_id,omitempty" jsonschema:"project UUID: operation.project_id. Required for a deployment."`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"how long to wait (default 10, max 12). Call again to keep waiting."`
	LogTail        int    `json:"log_tail,omitempty" jsonschema:"log lines to return (default 20, max 200)"`
}

// waitResult is the answer of wait_for.
type waitResult struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Status   string `json:"status"`
	Terminal bool   `json:"terminal"`
	TimedOut bool   `json:"timed_out"`
	WaitedMS int64  `json:"waited_ms"`
	// ErrorMessage is the deployment's error_message or the service's
	// last_error.
	ErrorMessage string   `json:"error_message,omitempty"`
	LogTail      []string `json:"log_tail"`
	// LogSource says where LogTail came from: deployment_log,
	// container_log, or none.
	LogSource string `json:"log_source"`
	LogError  string `json:"log_error,omitempty"`
}

// waitState is one status read.
type waitState struct {
	status, errMsg, dockerID string
	terminal                 bool
}

func (s *Server) registerWait() {
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name: "wait_for",
		Description: "Wait for a deployment or service a mutation started (its operation: kind, id, project_id) " +
			"until it reaches a terminal status or about 10 seconds pass. Returns status, error_message, and a " +
			"log tail. If timed_out is true it is still going; call again.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in waitForInput) (*mcp.CallToolResult, any, error) {
		return s.waitFor(ctx, in), nil, nil
	})
}

func (s *Server) waitFor(ctx context.Context, in waitForInput) *mcp.CallToolResult {
	if err := required("kind", in.Kind, "id", in.ID); err != nil {
		return errorResult(err)
	}
	switch in.Kind {
	case kindDeployment:
		if err := required("project_id", in.ProjectID); err != nil {
			return errorResult(err)
		}
	case kindService:
	default:
		return errorResult(&toolError{Status: http.StatusBadRequest, Code: codeBadRequest,
			Message: fmt.Sprintf("kind must be deployment or service, not %q", in.Kind)})
	}
	wait := defaultWait
	if in.TimeoutSeconds > 0 {
		wait = min(time.Duration(in.TimeoutSeconds)*time.Second, maxWait)
	}
	tail := defaultLogTail
	if in.LogTail > 0 {
		tail = min(in.LogTail, maxLogTail)
	}

	start := time.Now()
	wctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	var st waitState
	timedOut := false
	for {
		next, err := s.readState(wctx, in)
		if err != nil {
			if wctx.Err() != nil && ctx.Err() == nil && st.status != "" {
				timedOut = true
				break
			}
			return errorResult(err)
		}
		st = next
		if st.terminal {
			break
		}
		select {
		case <-wctx.Done():
			timedOut = true
		case <-time.After(waitPollInterval):
		}
		if timedOut {
			break
		}
	}

	out := waitResult{
		Kind: in.Kind, ID: in.ID, Status: st.status, Terminal: st.terminal, TimedOut: timedOut,
		WaitedMS: time.Since(start).Milliseconds(), ErrorMessage: st.errMsg, LogTail: []string{}, LogSource: "none",
	}
	lctx, lcancel := context.WithTimeout(context.WithoutCancel(ctx), logReadTimeout)
	defer lcancel()
	s.attachLogs(lctx, in, st, tail, &out)

	raw, err := json.Marshal(out)
	if err != nil {
		return errorResult(err)
	}
	return okResult(raw)
}

func (s *Server) readState(ctx context.Context, in waitForInput) (waitState, error) {
	if in.Kind == kindDeployment {
		raw, err := s.api.do(ctx, http.MethodGet, "/projects/"+seg(in.ProjectID)+"/deployments/"+seg(in.ID), nil, nil)
		if err != nil {
			return waitState{}, err
		}
		var d struct {
			Deployment struct {
				Status       string `json:"status"`
				ErrorMessage string `json:"error_message"`
			} `json:"deployment"`
			Terminal bool `json:"terminal"`
		}
		if err := json.Unmarshal(raw, &d); err != nil {
			return waitState{}, fmt.Errorf("decode deployment: %w", err)
		}
		return waitState{status: d.Deployment.Status, errMsg: d.Deployment.ErrorMessage, terminal: d.Terminal}, nil
	}
	raw, err := s.api.do(ctx, http.MethodGet, "/services/"+seg(in.ID), nil, nil)
	if err != nil {
		return waitState{}, err
	}
	var sv struct {
		Service struct {
			Status    string `json:"status"`
			LastError string `json:"last_error"`
		} `json:"service"`
		Container *struct {
			DockerID string `json:"docker_id"`
		} `json:"container"`
	}
	if err := json.Unmarshal(raw, &sv); err != nil {
		return waitState{}, fmt.Errorf("decode service: %w", err)
	}
	st := waitState{status: sv.Service.Status, errMsg: sv.Service.LastError}
	switch st.status {
	case "running", statusFailed, "stopped":
		st.terminal = true
	}
	if sv.Container != nil {
		st.dockerID = sv.Container.DockerID
	}
	return st, nil
}

// attachLogs adds the deployment log tail, or the service container's log
// tail. A failure to read logs is reported in log_error, not as a tool error.
func (s *Server) attachLogs(ctx context.Context, in waitForInput, st waitState, tail int, out *waitResult) {
	q := url.Values{"tail": []string{strconv.Itoa(tail)}}
	if in.Kind == kindDeployment {
		raw, err := s.api.do(ctx, http.MethodGet, "/projects/"+seg(in.ProjectID)+"/deployments/"+seg(in.ID)+"/logs", q, nil)
		if err != nil {
			out.LogError = logErr(err)
			return
		}
		var l struct {
			Lines []struct {
				Level   string `json:"level"`
				Message string `json:"message"`
			} `json:"lines"`
		}
		if err := json.Unmarshal(raw, &l); err != nil {
			out.LogError = "decode deployment logs: " + err.Error()
			return
		}
		for _, line := range l.Lines {
			out.LogTail = append(out.LogTail, line.Level+": "+line.Message)
		}
		out.LogSource = "deployment_log"
		return
	}
	if st.dockerID == "" {
		return
	}
	raw, err := s.api.do(ctx, http.MethodGet, "/containers/"+seg(st.dockerID)+"/logs", q, nil)
	if err != nil {
		out.LogError = logErr(err)
		return
	}
	var l struct {
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal(raw, &l); err != nil {
		out.LogError = "decode container logs: " + err.Error()
		return
	}
	out.LogTail = append(out.LogTail, l.Lines...)
	out.LogSource = "container_log"
}

func logErr(err error) string {
	var ae *apiError
	if errors.As(err, &ae) {
		p := errorPayloadOf(err)
		return fmt.Sprintf("%d %s: %s", ae.Status, p.Code, p.Message)
	}
	return err.Error()
}
