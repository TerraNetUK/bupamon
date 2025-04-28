package main

import "time"

// Helper functions to calculate statistics
func findMin(values []float64) float64 {
	if len(values) == 0 {
		return -1
	}
	minVal := values[0]
	for _, v := range values {
		if v < minVal {
			minVal = v
		}
	}
	return minVal
}

func findMax(values []float64) float64 {
	if len(values) == 0 {
		return -1
	}
	maxVal := values[0]
	for _, v := range values {
		if v > maxVal {
			maxVal = v
		}
	}
	return maxVal
}

func calculateAvg(values []float64) float64 {
	if len(values) == 0 {
		return -1
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// Convert duration to seconds for field naming
func durationToSeconds(d time.Duration) int64 {
	return int64(d.Seconds())
}
