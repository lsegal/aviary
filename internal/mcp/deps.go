package mcp

import (
	"context"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/browser"
	"github.com/lsegal/aviary/internal/channels"
	"github.com/lsegal/aviary/internal/memory"
	"github.com/lsegal/aviary/internal/scheduler"
)

// Deps holds the runtime dependencies injected into MCP tool handlers.
// Fields are nil until the relevant phase initializes them.
type Deps struct {
	Agents    *agent.Manager
	Scheduler *scheduler.Scheduler
	Memory    *memory.Manager
	Channels  *channels.Manager
	Browser   *browser.Manager
	Auth      *auth.FileStore // credential store; nil until server starts
	Upgrade   func(context.Context, string) error
}

// globalDeps is set by the server at startup.
var globalDeps = &Deps{}

// depsSet is true once SetDeps has been called explicitly (by the server or
// by tests). When true, ensureInProcessDeps skips auto-initialization so that
// deliberately-injected deps (including nil fields) are preserved.
var depsSet bool

// SetDeps replaces the global deps. Called once by the server before serving.
func SetDeps(d *Deps) { globalDeps = d; depsSet = true }

// GetDeps returns the current deps.
func GetDeps() *Deps { return globalDeps }
