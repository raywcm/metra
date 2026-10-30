package platform

import "math"

// Maximum encoded, one-way PCIe bandwidth. Packet overhead is not estimated.
// Unsupported generations stay unknown rather than guessing a denominator.
func pcieCapacity(generation, lanes uint32) float64 {
	if lanes == 0 {
		return math.NaN()
	}
	perLane := 0.0
	switch generation {
	case 1:
		perLane = 250e6
	case 2:
		perLane = 500e6
	case 3:
		perLane = 8e9 * 128 / 130 / 8
	case 4:
		perLane = 16e9 * 128 / 130 / 8
	case 5:
		perLane = 32e9 * 128 / 130 / 8
	default:
		return math.NaN()
	}
	return perLane * float64(lanes)
}

func linkPercent(rate, capacity float64) float64 {
	if math.IsNaN(rate) || math.IsNaN(capacity) || rate < 0 || capacity <= 0 {
		return math.NaN()
	}
	return math.Min(100, rate/capacity*100)
}
