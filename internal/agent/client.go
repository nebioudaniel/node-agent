package agent

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"node-agent/internal/api"
	"node-agent/internal/specs"
)

// Sentinel errors returned by sendHeartbeat for 401 and 404.
var (
	ErrUnauthorized  = errors.New("unauthorized")
	ErrUnknownWorker = errors.New("unknown worker")
)

// Client is the agent's coordinator client. It owns identity, the HTTP client,
// and the heartbeat loop.
type Client struct {
	base              string
	http              *http.Client // 5s timeout
	id                *Identity
	dir               string
	log               *slog.Logger
	heartbeatInterval time.Duration // unexported; tests may set (default 5s)
}

// NewClient creates a Client. log defaults to slog.Default() if nil.
// heartbeatInterval defaults to 5s.
func NewClient(base, dataDir string, log *slog.Logger) *Client {
	// TODO: implement per spec in docs/offline/step-1.8-client.md
	return &Client{base: base, dir: dataDir, log: log}
}

// Register creates a new worker identity via the coordinator and saves it.
func (c *Client) Register(ctx context.Context, token string, s specs.Specs) error {
	// TODO: implement per spec in docs/offline/step-1.8-client.md
	return nil
}

// sendHeartbeat sends one heartbeat and returns the coordinator's response.
func (c *Client) sendHeartbeat(ctx context.Context, hb api.Heartbeat) (api.HeartbeatResponse, error) {
	// TODO: implement per spec in docs/offline/step-1.8-client.md
	return api.HeartbeatResponse{}, nil
}

// Run is the agent's main loop: load/register identity, then heartbeat forever
// until ctx is cancelled. Never exits because the coordinator is down.
func (c *Client) Run(ctx context.Context, token string, s specs.Specs) {
	// TODO: implement per spec in docs/offline/step-1.8-client.md
}
