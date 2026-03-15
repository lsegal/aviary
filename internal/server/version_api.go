package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/update"
)

var versionCheck = update.Check

func (s *Server) versionHandler(w http.ResponseWriter, _ *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	check, _ := versionCheck(ctx, nil)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(check)
}

func (s *Server) versionUpgradeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	version := strings.TrimSpace(body.Version)

	if update.EmulationActive() {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"started":  true,
			"emulated": true,
			"message":  "Emulated upgrade completed. No files were changed.",
		})
		return
	}
	if err := s.triggerUpgrade(r.Context(), version); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"started": true,
		"message": "Upgrade started. Aviary will restart automatically.",
	})
}
