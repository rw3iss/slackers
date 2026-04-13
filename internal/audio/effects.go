// internal/audio/effects.go
package audio

// EffectChain applies audio effects to PCM samples.
// Full implementation in Phase C.
type EffectChain struct{}

// NewEffectChain creates a pass-through effect chain.
func NewEffectChain() *EffectChain { return &EffectChain{} }

// Process applies effects in-place. Currently a no-op.
func (c *EffectChain) Process(samples []float32) {}
