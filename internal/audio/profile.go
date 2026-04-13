package audio

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type EffectProfile struct {
	Name     string      `json:"name"`
	Outgoing ChainConfig `json:"outgoing"`
	Incoming ChainConfig `json:"incoming"`
}

type ChainConfig struct {
	EQEnabled     bool       `json:"eq_enabled"`
	EQBands       [7]float32 `json:"eq_bands"`
	CompEnabled   bool       `json:"comp_enabled"`
	CompThreshold float32    `json:"comp_threshold"`
	CompRatio     float32    `json:"comp_ratio"`
	CompAttack    float32    `json:"comp_attack"`
	CompRelease   float32    `json:"comp_release"`
	CompMakeup    float32    `json:"comp_makeup"`
}

func DefaultProfile() EffectProfile {
	return EffectProfile{
		Name: "Default",
		Outgoing: ChainConfig{
			CompThreshold: -20, CompRatio: 4, CompAttack: 10, CompRelease: 100,
		},
		Incoming: ChainConfig{
			CompThreshold: -20, CompRatio: 2, CompAttack: 10, CompRelease: 100,
		},
	}
}

func ApplyToChain(chain *EffectChain, cfg ChainConfig) {
	chain.EQEnabled = cfg.EQEnabled
	chain.CompEnabled = cfg.CompEnabled
	for i, gain := range cfg.EQBands {
		chain.EQ.Bands[i].SetGain(gain)
	}
	chain.Comp.Threshold = cfg.CompThreshold
	chain.Comp.Ratio = cfg.CompRatio
	chain.Comp.AttackMs = cfg.CompAttack
	chain.Comp.ReleaseMs = cfg.CompRelease
	chain.Comp.MakeupGain = cfg.CompMakeup
}

func ChainToConfig(chain *EffectChain) ChainConfig {
	var bands [7]float32
	for i, b := range chain.EQ.Bands {
		bands[i] = b.Gain
	}
	return ChainConfig{
		EQEnabled:     chain.EQEnabled,
		EQBands:       bands,
		CompEnabled:   chain.CompEnabled,
		CompThreshold: chain.Comp.Threshold,
		CompRatio:     chain.Comp.Ratio,
		CompAttack:    chain.Comp.AttackMs,
		CompRelease:   chain.Comp.ReleaseMs,
		CompMakeup:    chain.Comp.MakeupGain,
	}
}

func LoadProfiles(configDir string) []EffectProfile {
	path := filepath.Join(configDir, "audio_profiles.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return []EffectProfile{DefaultProfile()}
	}
	var profiles []EffectProfile
	if err := json.Unmarshal(data, &profiles); err != nil || len(profiles) == 0 {
		return []EffectProfile{DefaultProfile()}
	}
	return profiles
}

func SaveProfiles(configDir string, profiles []EffectProfile) error {
	path := filepath.Join(configDir, "audio_profiles.json")
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
