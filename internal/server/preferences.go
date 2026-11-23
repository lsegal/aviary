package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/lsegal/aviary/internal/store"
)

// maxPreferencesBytes caps a navigation preferences update.
const maxPreferencesBytes = 64 << 10

// navPreferencesHandler reads (GET), replaces (PUT) or resets (DELETE) the
// web UI's navigation order and visibility (#1003).
func navPreferencesHandler(w http.ResponseWriter, r *http.Request) {
	var (
		prefs *store.Preferences
		err   error
	)
	switch r.Method {
	case http.MethodGet:
		prefs, err = store.ReadPreferences()
	case http.MethodPut:
		var nav store.NavPreferences
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPreferencesBytes)).Decode(&nav); err != nil {
			http.Error(w, "invalid navigation preferences", http.StatusBadRequest)
			return
		}
		prefs, err = store.UpdatePreferences(func(p *store.Preferences) {
			p.Navigation = cleanNavPreferences(nav)
		})
	case http.MethodDelete:
		prefs, err = store.UpdatePreferences(func(p *store.Preferences) {
			p.Navigation = cleanNavPreferences(store.NavPreferences{})
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err != nil {
		slog.Warn("server: navigation preferences", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(prefs.Navigation)
}

// cleanNavPreferences drops empty lists and duplicate or empty keys.
func cleanNavPreferences(nav store.NavPreferences) store.NavPreferences {
	clean := func(in map[string][]string) map[string][]string {
		out := map[string][]string{}
		for list, keys := range in {
			seen := map[string]bool{}
			var kept []string
			for _, k := range keys {
				if k != "" && !seen[k] {
					seen[k] = true
					kept = append(kept, k)
				}
			}
			if list != "" && len(kept) > 0 {
				out[list] = kept
			}
		}
		return out
	}
	return store.NavPreferences{Order: clean(nav.Order), Hidden: clean(nav.Hidden)}
}
