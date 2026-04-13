package audio

import "math"

type EffectChain struct {
	EQ          *Equalizer
	Comp        *Compressor
	EQEnabled   bool
	CompEnabled bool
}

func NewEffectChain() *EffectChain {
	return &EffectChain{
		EQ:   NewEqualizer(),
		Comp: NewCompressor(),
	}
}

func (c *EffectChain) Process(samples []float32) {
	if c.EQEnabled && c.EQ != nil {
		c.EQ.Process(samples)
	}
	if c.CompEnabled && c.Comp != nil {
		c.Comp.Process(samples)
	}
}

// ── Equalizer ──

type EQBand struct {
	Label  string
	Freq   float64
	Gain   float32
	Type   BiquadType
	filter BiquadFilter
}

type Equalizer struct {
	Bands [7]EQBand
}

func NewEqualizer() *Equalizer {
	eq := &Equalizer{
		Bands: [7]EQBand{
			{Label: "100Hz", Freq: 100, Type: BiquadLowShelf},
			{Label: "250Hz", Freq: 250, Type: BiquadPeaking},
			{Label: "500Hz", Freq: 500, Type: BiquadPeaking},
			{Label: "1kHz", Freq: 1000, Type: BiquadPeaking},
			{Label: "3kHz", Freq: 3000, Type: BiquadPeaking},
			{Label: "7kHz", Freq: 7000, Type: BiquadPeaking},
			{Label: "12kHz", Freq: 12000, Type: BiquadHighShelf},
		},
	}
	for i := range eq.Bands {
		eq.Bands[i].Recalc()
	}
	return eq
}

func (b *EQBand) Recalc() {
	b.filter.ComputeCoefficients(b.Type, b.Freq, float64(SampleRate), float64(b.Gain), 1.0)
}

func (b *EQBand) SetGain(g float32) {
	if g < -12 {
		g = -12
	}
	if g > 12 {
		g = 12
	}
	b.Gain = g
	b.Recalc()
}

func (eq *Equalizer) Process(samples []float32) {
	for i, s := range samples {
		v := float64(s)
		for b := range eq.Bands {
			v = eq.Bands[b].filter.Process(v)
		}
		samples[i] = float32(v)
	}
}

// ── Compressor ──

type Compressor struct {
	Threshold  float32
	Ratio      float32
	AttackMs   float32
	ReleaseMs  float32
	MakeupGain float32
	envelope   float32

	InputLevel    float32
	GainReduction float32
	OutputLevel   float32
}

func NewCompressor() *Compressor {
	return &Compressor{
		Threshold: -20,
		Ratio:     4,
		AttackMs:  10,
		ReleaseMs: 100,
	}
}

func (c *Compressor) Process(samples []float32) {
	attackCoeff := float32(math.Exp(-1.0 / (float64(c.AttackMs) * 0.001 * float64(SampleRate))))
	releaseCoeff := float32(math.Exp(-1.0 / (float64(c.ReleaseMs) * 0.001 * float64(SampleRate))))

	var inputSum, outputSum float32
	var maxGR float32

	for i, s := range samples {
		absS := s
		if absS < 0 {
			absS = -absS
		}
		inputDB := float32(-96.0)
		if absS > 1e-10 {
			inputDB = float32(20 * math.Log10(float64(absS)))
		}
		if inputDB > c.envelope {
			c.envelope = attackCoeff*c.envelope + (1-attackCoeff)*inputDB
		} else {
			c.envelope = releaseCoeff*c.envelope + (1-releaseCoeff)*inputDB
		}
		gr := float32(0)
		if c.envelope > c.Threshold && c.Ratio > 1 {
			excess := c.envelope - c.Threshold
			gr = excess - excess/c.Ratio
		}
		gainDB := -gr + c.MakeupGain
		gain := float32(math.Pow(10, float64(gainDB)/20.0))
		samples[i] = s * gain
		inputSum += s * s
		outputSum += samples[i] * samples[i]
		if gr > maxGR {
			maxGR = gr
		}
	}

	n := float32(len(samples))
	if n > 0 {
		c.InputLevel = dbFS(float32(math.Sqrt(float64(inputSum / n))))
		c.OutputLevel = dbFS(float32(math.Sqrt(float64(outputSum / n))))
		c.GainReduction = -maxGR
	}
}

func dbFS(rms float32) float32 {
	if rms < 1e-10 {
		return -96
	}
	return float32(20 * math.Log10(float64(rms)))
}
