package audio

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type CallRecord struct {
	CallID    string        `json:"call_id"`
	PeerID    string        `json:"peer_id"`
	PeerName  string        `json:"peer_name"`
	Started   time.Time     `json:"started"`
	Duration  time.Duration `json:"duration_ns"`
	Direction string        `json:"direction"`
}

type CallHistory struct {
	Calls []CallRecord `json:"calls"`
}

func LoadCallHistory(configDir string) *CallHistory {
	path := filepath.Join(configDir, "call_history.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return &CallHistory{}
	}
	var h CallHistory
	if err := json.Unmarshal(data, &h); err != nil {
		return &CallHistory{}
	}
	return &h
}

func (h *CallHistory) Add(r CallRecord) {
	h.Calls = append(h.Calls, r)
	if len(h.Calls) > 50 {
		h.Calls = h.Calls[len(h.Calls)-50:]
	}
}

func (h *CallHistory) Save(configDir string) error {
	path := filepath.Join(configDir, "call_history.json")
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
