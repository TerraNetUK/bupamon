package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"sync/atomic"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

// ProcessManager Process manager to handle fping restarts
type ProcessManager struct {
	config        *Config
	logger        *log.Logger
	cmd           *exec.Cmd
	stdout        io.ReadCloser
	stderr        io.ReadCloser
	ctx           context.Context
	cancel        context.CancelFunc
	highLossCount int32
	windowSizes   []time.Duration
	targetStats   map[string]*TargetStats
	client        influxdb2.Client
}

// NewProcessManager Create a new process manager
func NewProcessManager(config *Config, logger *log.Logger, apiClient influxdb2.Client) *ProcessManager {
	ctx, cancel := context.WithCancel(context.Background())

	var windowSizes []time.Duration
	// Parse window sizes
	for _, ws := range config.BupaMon.WindowSizes {
		d, err := ParseDuration(ws)
		if err != nil {
			logger.Fatalf("Invalid window size: %s - %v", ws, err)
		}
		logger.Printf("Target window size: %s", ws)
		windowSizes = append(windowSizes, d)
	}

	// Load targets
	targets := config.Targets.Hosts
	if config.Targets.File != "" {
		fileTargets, err := LoadTargetsFromFile(config.Targets.File)
		if err != nil {
			logger.Printf("Warning: Error loading targets from file: %v", err)
		} else {
			targets = append(targets, fileTargets...)
		}
	}

	// Initialize stats for each target
	targetStats := make(map[string]*TargetStats)
	for _, target := range targets {
		logger.Printf("Target stats for: %s", target)
		targetStats[target] = &TargetStats{
			tracker:         NewRollingStatsTracker(windowSizes),
			absoluteMinimum: -1,
		}
	}

	logger.Printf("Monitoring %d targets with %d window sizes from source '%s'", len(targets), len(windowSizes), config.BupaMon.SourceHost)

	return &ProcessManager{
		config:      config,
		logger:      logger,
		ctx:         ctx,
		cancel:      cancel,
		windowSizes: windowSizes,
		targetStats: targetStats,
		client:      apiClient,
	}
}

// Start the fping process
func (pm *ProcessManager) Start() error {
	// Prepare fping command
	args := append(pm.config.FPing.Args, pm.getTargets()...)
	pm.cmd = exec.CommandContext(pm.ctx, pm.config.FPing.Path, args...)

	// Set the working directory if specified
	if pm.config.BupaMon.WorkingDir != "" {
		pm.cmd.Dir = pm.config.BupaMon.WorkingDir
	}

	// Setup pipes
	var err error
	pm.stdout, err = pm.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("error creating stdout pipe: %v", err)
	}

	pm.stderr, err = pm.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("error creating stderr pipe: %v", err)
	}

	// Start the command
	pm.logger.Printf("Starting fping with args: %v", args)

	err = pm.cmd.Start()
	if err != nil {
		return fmt.Errorf("error starting fping: %v", err)
	}

	pm.logger.Printf("Processing fping stdout")
	// Process stdout for regular pings
	go pm.processStdout()

	pm.logger.Printf("Processing fping stderr")
	// Process stderr for summary statistics
	go pm.processStderr()

	// Monitor the command completion
	go func() {
		err := pm.cmd.Wait()
		if err != nil {
			pm.logger.Printf("fping exited: %v", err)
		}
	}()

	return nil
}

// Get list of targets
func (pm *ProcessManager) getTargets() []string {
	targets := make([]string, 0, len(pm.targetStats))
	for target := range pm.targetStats {
		targets = append(targets, target)
	}
	return targets
}

// Process stdout for regular pings
func (pm *ProcessManager) processStdout() {
	// Use the regex for parsing fping output
	re := regexp.MustCompile(`\[(\d+\.\d+)] ([0-9.]+)\s+: \[\d+], \d+ bytes, ([0-9.]+) ms \(([0-9.]+) avg, (\d+)% loss\)`)
	//re := regexp.MustCompile(`\[(\d+\.\d+)] ([0-9.]+)\s+: \[\d+], \d+ bytes, ([0-9.]+) ms \(([0-9.]+) avg, (\d+)% loss\)`)

	writeAPI := pm.client.WriteAPI(pm.config.InfluxDB.Org, pm.config.InfluxDB.Bucket)

	scanner := bufio.NewScanner(pm.stdout)

	for scanner.Scan() {
		line := scanner.Text()
		matches := re.FindStringSubmatch(line)

		if len(matches) >= 6 {
			timestamp, _ := strconv.ParseFloat(matches[1], 64)
			target := matches[2]
			current, _ := strconv.ParseFloat(matches[3], 64)
			fpingAvg, _ := strconv.ParseFloat(matches[4], 64)
			loss, _ := strconv.Atoi(matches[5])

			// Check for high packet loss
			if loss >= pm.config.FPing.RestartThreshold {
				atomic.AddInt32(&pm.highLossCount, 1)
				pm.logger.Printf("Warning: High packet loss (%d%%) detected for target %s", loss, target)

				// If we've reached consecutive threshold, restart fping
				if atomic.LoadInt32(&pm.highLossCount) >= int32(pm.config.FPing.ConsecutiveThreshold) {
					pm.logger.Printf("Consecutive high packet loss threshold reached (%d), restarting fping",
						pm.config.FPing.ConsecutiveThreshold)

					// Restart in a new goroutine to avoid blocking
					go pm.Restart()
					return // Exit this goroutine since we're restarting
				}
			} else {
				// Reset high loss counter if we see good results
				atomic.StoreInt32(&pm.highLossCount, 0)
			}

			stats, ok := pm.targetStats[target]
			if !ok {
				pm.logger.Printf("Warning: Received ping for unknown target: %s", target)
				continue
			}

			// Add to tracker
			stats.tracker.Add(current)

			// Update absolute minimum and maximum
			if stats.absoluteMinimum < 0 || current < stats.absoluteMinimum {
				stats.absoluteMinimum = current
			}
			if stats.absoluteMaximum < 0 || current > stats.absoluteMaximum {
				stats.absoluteMaximum = current
			}

			// Create a point for InfluxDB
			fields := map[string]interface{}{
				"current_ms":      current,
				"fping_avg_ms":    fpingAvg, // fping's own running average
				"absolute_min_ms": stats.absoluteMinimum,
				"absolute_max_ms": stats.absoluteMaximum,
				"loss_pct":        loss, // fping's packet loss percentage
			}

			// Add rolling stats for each window size
			for _, window := range pm.windowSizes {
				windowSecs := durationToSeconds(window)
				fields[fmt.Sprintf("min_%d_ms", windowSecs)] = stats.tracker.GetStat(MinStat, window)
				fields[fmt.Sprintf("max_%d_ms", windowSecs)] = stats.tracker.GetStat(MaxStat, window)
				fields[fmt.Sprintf("avg_%d_ms", windowSecs)] = stats.tracker.GetStat(AvgStat, window)
			}

			point := influxdb2.NewPoint(
				pm.config.InfluxDB.Measurement,
				map[string]string{
					"source": pm.config.BupaMon.SourceHost,
					"target": target,
				},
				fields,
				time.Unix(int64(timestamp), int64((timestamp-float64(int64(timestamp)))*1e9)),
			)

			// Write to InfluxDB
			writeAPI.WritePoint(point)

			// Log output (if verbose)
			if pm.config.Logging.Enabled {
				pm.logger.Printf("Target: %s, Current: %.2f ms, Absolute Min: %.2f ms",
					target, current, stats.absoluteMinimum)
				for _, window := range pm.windowSizes {
					windowSecs := durationToSeconds(window)
					minVal := stats.tracker.GetStat(MinStat, window)
					maxVal := stats.tracker.GetStat(MaxStat, window)
					avgVal := stats.tracker.GetStat(AvgStat, window)

					// Use human-readable format in logs
					pm.logger.Printf("  %v window (%ds) - Min: %.2f ms, Avg: %.2f ms, Max: %.2f ms",
						window, windowSecs, minVal, avgVal, maxVal)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		pm.logger.Printf("Error reading from stdout: %v", err)
	}
}

// Process stderr for summary stats
func (pm *ProcessManager) processStderr() {
	summaryRe := regexp.MustCompile(`([0-9.]+)\s+: xmt/rcv/%loss = \d+/\d+/\d+%, min/avg/max = ([0-9.]+)/([0-9.]+)/([0-9.]+)`)

	// Process stderr for the summary statistics
	scanner := bufio.NewScanner(pm.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		matches := summaryRe.FindStringSubmatch(line)

		if len(matches) >= 5 {
			target := matches[1]
			fpingMin, _ := strconv.ParseFloat(matches[2], 64) // fping's min value
			fpingAvg, _ := strconv.ParseFloat(matches[3], 64) // fping's avg value
			fpingMax, _ := strconv.ParseFloat(matches[4], 64) // fping's max value

			// Create a summary point
			point := influxdb2.NewPoint(
				fmt.Sprintf("%s_summary", pm.config.InfluxDB.Measurement),
				map[string]string{
					"source": pm.config.BupaMon.SourceHost,
					"target": target,
				},
				map[string]interface{}{
					"fping_min_ms": fpingMin, // Renamed to clarify these are fping's own calculations
					"fping_avg_ms": fpingAvg,
					"fping_max_ms": fpingMax,
				},
				time.Now(),
			)

			if pm.config.Logging.Enabled {
				pm.logger.Printf("Summary - Target: %s, fping Min: %.2f ms, fping Avg: %.2f ms, fping Max: %.2f ms",
					target, fpingMin, fpingAvg, fpingMax)
			}

			// Write to InfluxDB
			writeAPI := pm.client.WriteAPI(pm.config.InfluxDB.Org, pm.config.InfluxDB.Bucket)
			writeAPI.WritePoint(point)

			// Flush any remaining writes
			writeAPI.Flush()
		}
	}

	if err := scanner.Err(); err != nil {
		pm.logger.Printf("Error reading from stdout: %v", err)
	}
}

// Restart the fping process
func (pm *ProcessManager) Restart() {
	// Cancel the current context to kill the process
	pm.cancel()

	// Wait a moment for cleanup
	time.Sleep(1 * time.Second)

	// Create a new context for the new process
	pm.ctx, pm.cancel = context.WithCancel(context.Background())

	// Reset high loss counter
	atomic.StoreInt32(&pm.highLossCount, 0)

	// Start a new process
	err := pm.Start()
	if err != nil {
		pm.logger.Printf("Error restarting fping: %v", err)
	} else {
		pm.logger.Printf("Successfully restarted fping")
	}
}

// Stop the fping process
func (pm *ProcessManager) Stop() {
	pm.cancel()
}
