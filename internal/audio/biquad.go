package audio

import "math"

type BiquadType int

const (
	BiquadLowShelf BiquadType = iota
	BiquadPeaking
	BiquadHighShelf
)

type BiquadFilter struct {
	b0, b1, b2 float64
	a1, a2     float64
	z1, z2     float64
}

func (f *BiquadFilter) ComputeCoefficients(typ BiquadType, freq, sampleRate, gainDB, q float64) {
	A := math.Pow(10, gainDB/40.0)
	w0 := 2 * math.Pi * freq / sampleRate
	sinW0 := math.Sin(w0)
	cosW0 := math.Cos(w0)
	alpha := sinW0 / (2 * q)

	var b0, b1, b2, a0, a1, a2 float64

	switch typ {
	case BiquadLowShelf:
		sqrtA := math.Sqrt(A)
		b0 = A * ((A + 1) - (A-1)*cosW0 + 2*sqrtA*alpha)
		b1 = 2 * A * ((A - 1) - (A+1)*cosW0)
		b2 = A * ((A + 1) - (A-1)*cosW0 - 2*sqrtA*alpha)
		a0 = (A + 1) + (A-1)*cosW0 + 2*sqrtA*alpha
		a1 = -2 * ((A - 1) + (A+1)*cosW0)
		a2 = (A + 1) + (A-1)*cosW0 - 2*sqrtA*alpha
	case BiquadPeaking:
		b0 = 1 + alpha*A
		b1 = -2 * cosW0
		b2 = 1 - alpha*A
		a0 = 1 + alpha/A
		a1 = -2 * cosW0
		a2 = 1 - alpha/A
	case BiquadHighShelf:
		sqrtA := math.Sqrt(A)
		b0 = A * ((A + 1) + (A-1)*cosW0 + 2*sqrtA*alpha)
		b1 = -2 * A * ((A - 1) + (A+1)*cosW0)
		b2 = A * ((A + 1) + (A-1)*cosW0 - 2*sqrtA*alpha)
		a0 = (A + 1) - (A-1)*cosW0 + 2*sqrtA*alpha
		a1 = 2 * ((A - 1) - (A+1)*cosW0)
		a2 = (A + 1) - (A-1)*cosW0 - 2*sqrtA*alpha
	}

	f.b0 = b0 / a0
	f.b1 = b1 / a0
	f.b2 = b2 / a0
	f.a1 = a1 / a0
	f.a2 = a2 / a0
}

func (f *BiquadFilter) Process(in float64) float64 {
	out := f.b0*in + f.z1
	f.z1 = f.b1*in - f.a1*out + f.z2
	f.z2 = f.b2*in - f.a2*out
	return out
}

func (f *BiquadFilter) Reset() {
	f.z1 = 0
	f.z2 = 0
}
