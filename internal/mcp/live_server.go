package mcp

import (
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/config"
)

var (
	liveServerMu sync.RWMutex
	liveServer   *sdkmcp.Server
)

// SetLiveServer records the process MCP server so runtime tools can be synced
// when config changes without requiring a server restart.
func SetLiveServer(s *sdkmcp.Server) {
	liveServerMu.Lock()
	liveServer = s
	liveServerMu.Unlock()
}

// SyncLiveServer refreshes config-driven runtime tools on the current process MCP server.
func SyncLiveServer(cfg *config.Config) {
	liveServerMu.RLock()
	s := liveServer
	liveServerMu.RUnlock()
	if s == nil {
		return
	}
	syncSkillTools(s, cfg)
}

// RefreshLiveServer registers every tool on the current process MCP server
// again. A hot reload replaces changed code without rerunning startup, so this
// is how tools a reload adds or changes reach the MCP server; registering a
// tool replaces the one with the same name, and connected sessions are told
// the list changed.
func RefreshLiveServer() {
	liveServerMu.RLock()
	s := liveServer
	liveServerMu.RUnlock()
	if s == nil {
		return
	}
	Register(s)
}
