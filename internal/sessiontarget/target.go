// Package sessiontarget manages persisted session delivery targets.
package sessiontarget

import (
	"log/slog"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/channels"
	"github.com/lsegal/aviary/internal/store"
)

// Register wires the session's text and media delivery to the configured
// target described by the sidecar entry.
func Register(agentID, agentName, sessionID string, target store.SessionChannel, mgr *channels.Manager) {
	if mgr == nil || sessionID == "" || target.Type == "" || target.ID == "" {
		return
	}

	agent.RegisterSessionDelivery(agentID, sessionID, target.Type, target.ID, func(text string) {
		var err error
		if target.ConfiguredID != "" {
			err = mgr.SendOnConfiguredChannel(agentName, target.Type, target.ConfiguredID, target.ID, text)
		} else {
			err = mgr.RouteDelivery(target.Type, target.ID, text)
		}
		if err != nil {
			slog.Warn("session target: failed to deliver text", "agent", agentName, "session", sessionID, "type", target.Type, "configured_id", target.ConfiguredID, "target", target.ID, "err", err)
		}
	})

	agent.RegisterSessionMediaDelivery(agentID, sessionID, target.Type, target.ID, func(caption, path string) {
		deliverPath := path
		if staged, err := channels.StageOutgoingMedia(target.Type, path); err == nil {
			deliverPath = staged
		} else {
			slog.Warn("session target: failed to stage outgoing media", "agent", agentName, "session", sessionID, "type", target.Type, "path", path, "err", err)
		}

		var err error
		if target.ConfiguredID != "" {
			err = mgr.SendMediaOnConfiguredChannel(agentName, target.Type, target.ConfiguredID, target.ID, caption, deliverPath)
		} else {
			err = mgr.RouteMediaDelivery(target.Type, target.ID, caption, deliverPath)
		}
		if err != nil {
			slog.Warn("session target: failed to deliver media", "agent", agentName, "session", sessionID, "type", target.Type, "configured_id", target.ConfiguredID, "target", target.ID, "err", err)
		}
	})
}

// Set persists a single target in the session sidecar and registers it with
// the live channel manager when available.
func Set(agentID, agentName, sessionID string, target store.SessionChannel, mgr *channels.Manager) error {
	if err := store.SetSessionChannel(agentID, sessionID, target.Type, target.ConfiguredID, target.ID); err != nil {
		return err
	}
	Register(agentID, agentName, sessionID, target, mgr)
	return nil
}
