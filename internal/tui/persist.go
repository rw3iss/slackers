package tui

// This file holds the small config-persistence helpers that used to
// live in model.go. They were extracted as part of the Phase C
// "split model.go" pass — see docs/phase-c-plan-2026-04-08.md.
//
// Everything here is a thin wrapper around config.SaveDebounced or
// a clone of config state into a Model-owned map; no business logic.

import "github.com/rw3iss/slackers/internal/config"

// saveLastChannel remembers the most recently opened channel so the
// next launch can re-open it automatically.
func (m *Model) saveLastChannel(channelID string) {
	m.cfg.LastChannelID = channelID
	config.SaveDebounced(m.cfg) // fire-and-forget, don't block the UI
}

// loadLastSeen initializes lastSeen from persisted config.
func loadLastSeen(cfg *config.Config) map[string]string {
	if cfg.LastSeenTS != nil && len(cfg.LastSeenTS) > 0 {
		// Clone it so we don't mutate the config map directly.
		m := make(map[string]string, len(cfg.LastSeenTS))
		for k, v := range cfg.LastSeenTS {
			m[k] = v
		}
		return m
	}
	return make(map[string]string)
}

// persistLastSeen saves lastSeen timestamps to config (fire-and-forget).
func (m *Model) persistLastSeen() {
	m.cfg.LastSeenTS = make(map[string]string, len(m.lastSeen))
	for k, v := range m.lastSeen {
		if v != "0" && v != "" {
			m.cfg.LastSeenTS[k] = v
		}
	}
	config.SaveDebounced(m.cfg)
}
