package audio

import "math"

func MeterBar(level float32, width int, rangeDB float32) string {
	if level < -rangeDB {
		level = -rangeDB
	}
	if level > 0 {
		level = 0
	}
	frac := (level + rangeDB) / rangeDB
	filled := int(frac * float32(width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	result := ""
	for i := 0; i < width; i++ {
		if i < filled {
			result += "█"
		} else {
			result += "░"
		}
	}
	return result
}

func GainReductionBar(gr float32, width int, rangeDB float32) string {
	absGR := -gr
	if absGR < 0 {
		absGR = 0
	}
	if absGR > rangeDB {
		absGR = rangeDB
	}
	frac := absGR / rangeDB
	filled := int(frac * float32(width))
	result := ""
	for i := 0; i < width; i++ {
		if i >= width-filled {
			result += "█"
		} else {
			result += "░"
		}
	}
	return result
}

func RMSLevel(samples []float32) float32 {
	if len(samples) == 0 {
		return -96
	}
	var sum float64
	for _, s := range samples {
		sum += float64(s) * float64(s)
	}
	rms := math.Sqrt(sum / float64(len(samples)))
	if rms < 1e-10 {
		return -96
	}
	return float32(20 * math.Log10(rms))
}
