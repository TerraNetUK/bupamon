package main

import (
	"sync"
	"time"
)

// StatType defines the type of statistic to track
type StatType string

const (
	MinStat StatType = "min"
	MaxStat StatType = "max"
	AvgStat StatType = "avg"
)

// Measurement represents a single latency measurement
type Measurement struct {
	Value     float64
	Timestamp time.Time
}

// RollingStatsTracker tracks statistics over configurable rolling windows
type RollingStatsTracker struct {
	measurements []Measurement
	windowSizes  []time.Duration
	mutex        sync.RWMutex
}

// NewRollingStatsTracker creates a new stats tracker with specified window sizes
func NewRollingStatsTracker(windowSizes []time.Duration) *RollingStatsTracker {
	return &RollingStatsTracker{
		measurements: make([]Measurement, 0),
		windowSizes:  windowSizes,
	}
}

// Add adds a new measurement
func (r *RollingStatsTracker) Add(value float64) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	now := time.Now()
	r.measurements = append(r.measurements, Measurement{
		Value:     value,
		Timestamp: now,
	})

	// Clean up measurements outside the largest window
	if len(r.windowSizes) > 0 {
		maxWindow := r.windowSizes[len(r.windowSizes)-1]
		r.cleanup(now, maxWindow)
	}
}

// GetStat returns the calculated statistic for a specific window size
func (r *RollingStatsTracker) GetStat(statType StatType, window time.Duration) float64 {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	now := time.Now()
	cutoff := now.Add(-window)

	var values []float64
	for _, m := range r.measurements {
		if m.Timestamp.After(cutoff) {
			values = append(values, m.Value)
		}
	}

	if len(values) == 0 {
		return -1 // No values in window
	}

	switch statType {
	case MinStat:
		return findMin(values)
	case MaxStat:
		return findMax(values)
	case AvgStat:
		return calculateAvg(values)
	default:
		return -1
	}
}

// cleanup removes measurements older than the specified window
func (r *RollingStatsTracker) cleanup(now time.Time, window time.Duration) {
	cutoff := now.Add(-window)

	newStart := 0
	for i, m := range r.measurements {
		if m.Timestamp.After(cutoff) {
			newStart = i
			break
		}
	}

	if newStart > 0 {
		r.measurements = r.measurements[newStart:]
	}
}

// TargetStats holds stats for a specific target
type TargetStats struct {
	tracker         *RollingStatsTracker
	absoluteMinimum float64
	absoluteMaximum float64
}
