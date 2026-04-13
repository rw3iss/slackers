package audio

// VAD performs simple energy-based voice activity detection.
type VAD struct {
	Threshold   float32 // dBFS threshold for speech detection
	HoldMs      int     // ms to hold "active" after last voice frame
	holdSamples int
	silentFor   int
}

func NewVAD(thresholdDB float32, holdMs int) *VAD {
	return &VAD{
		Threshold:   thresholdDB,
		HoldMs:      holdMs,
		holdSamples: holdMs * SampleRate / 1000,
	}
}

// IsVoice returns true if the frame contains voice activity.
func (v *VAD) IsVoice(samples []float32) bool {
	level := RMSLevel(samples)
	if level > v.Threshold {
		v.silentFor = 0
		return true
	}
	v.silentFor += len(samples)
	return v.silentFor < v.holdSamples
}
