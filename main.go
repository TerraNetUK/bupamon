package main

import (
	"bufio"
	"fmt"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"syscall"
	"time"
)

func main() {
	// Load configuration
	config, err := LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	// Set up logging
	var logger *log.Logger
	if config.Logging.Enabled {
		logFile, err := os.OpenFile(config.Logging.Logfile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			log.Fatalf("Error opening log file: %v", err)
		}
		defer func(logFile *os.File) {
			err := logFile.Close()
			if err != nil {
				log.Fatalf("Error closing log file: %v", err)
			}
		}(logFile)
		logger = log.New(logFile, "BupaMon: ", log.Ldate|log.Ltime|log.Lshortfile)
	} else {
		logger = log.New(os.Stdout, "BupaMon: ", log.Ldate|log.Ltime|log.Lshortfile)
	}

	// Parse window sizes
	var windowSizes []time.Duration
	for _, ws := range config.BupaMon.WindowSizes {
		d, err := ParseDuration(ws)
		if err != nil {
			logger.Fatalf("Invalid window size: %s - %v", ws, err)
		}
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

	logger.Printf("Monitoring %d targets with %d window sizes from source '%s'",
		len(targets), len(windowSizes), config.BupaMon.SourceHost)

	// Initialize stats for each target
	targetStats := make(map[string]*TargetStats)
	for _, target := range targets {
		targetStats[target] = &TargetStats{
			tracker:         NewRollingStatsTracker(windowSizes),
			absoluteMinimum: -1,
		}
	}

	// Set up InfluxDB client
	options := influxdb2.DefaultOptions()
	options.SetBatchSize(20)       // Reduce batch size
	options.SetRetryInterval(5000) // Retry every 5 seconds
	options.SetMaxRetries(5)       // Retry 5 times
	options.SetLogLevel(3)         // More verbose logging

	influxURL := fmt.Sprintf("https://%s:%d", config.InfluxDB.Host, config.InfluxDB.Port)
	client := influxdb2.NewClientWithOptions(influxURL, config.InfluxDB.Token, options)
	defer client.Close()

	writeAPI := client.WriteAPI(config.InfluxDB.Org, config.InfluxDB.Bucket)

	// Regex for parsing fping output
	re := regexp.MustCompile(`\[(\d+\.\d+)] ([0-9.]+)\s+: \[\d+], \d+ bytes, ([0-9.]+) ms \(([0-9.]+) avg, (\d+)% loss\)`)
	summaryRe := regexp.MustCompile(`([0-9.]+)\s+: xmt/rcv/%loss = \d+/\d+/\d+%, min/avg/max = ([0-9.]+)/([0-9.]+)/([0-9.]+)`)

	// Set up signal handling for graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		logger.Println("Received shutdown signal, flushing remaining writes...")
		writeAPI.Flush()
		os.Exit(0)
	}()

	// Prepare fping command
	args := append(config.FPing.Args, targets...)
	cmd := exec.Command(config.FPing.Path, args...)
	logger.Printf("Starting fping with args: %v", args)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logger.Fatalf("Error creating StdoutPipe: %v", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		logger.Fatalf("Error creating StderrPipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		logger.Fatalf("Error starting fping: %v", err)
	}

	// Process stdout for regular pings
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			matches := re.FindStringSubmatch(line)

			if len(matches) >= 6 {
				timestamp, _ := strconv.ParseFloat(matches[1], 64)
				target := matches[2]
				current, _ := strconv.ParseFloat(matches[3], 64)
				fpingAvg, _ := strconv.ParseFloat(matches[4], 64)
				loss, _ := strconv.Atoi(matches[5])

				stats, ok := targetStats[target]
				if !ok {
					logger.Printf("Warning: Received ping for unknown target: %s", target)
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
				for _, window := range windowSizes {
					windowStr := window.String()
					fields[fmt.Sprintf("min_%s_ms", windowStr)] = stats.tracker.GetStat(MinStat, window)
					fields[fmt.Sprintf("max_%s_ms", windowStr)] = stats.tracker.GetStat(MaxStat, window)
					fields[fmt.Sprintf("avg_%s_ms", windowStr)] = stats.tracker.GetStat(AvgStat, window)
				}

				point := influxdb2.NewPoint(
					config.InfluxDB.Measurement,
					map[string]string{
						"source": config.BupaMon.SourceHost,
						"target": target,
					},
					fields,
					time.Unix(int64(timestamp), int64((timestamp-float64(int64(timestamp)))*1e9)),
				)

				// Write to InfluxDB
				writeAPI.WritePoint(point)

				// Log output (if verbose)
				if config.Logging.Enabled {
					logger.Printf("Target: %s, Current: %.2f ms, Absolute Min: %.2f ms",
						target, current, stats.absoluteMinimum)
				}
			}
		}

		if err := scanner.Err(); err != nil {
			logger.Printf("Error reading from stdout: %v", err)
		}
	}()

	// Process stderr for the summary statistics
	go func() {
		scanner := bufio.NewScanner(stderr)
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
					fmt.Sprintf("%s_summary", config.InfluxDB.Measurement),
					map[string]string{
						"source": config.BupaMon.SourceHost,
						"target": target,
					},
					map[string]interface{}{
						"fping_min_ms": fpingMin, // Renamed to clarify these are fping's own calculations
						"fping_avg_ms": fpingAvg,
						"fping_max_ms": fpingMax,
					},
					time.Now(),
				)

				// Write to InfluxDB
				writeAPI.WritePoint(point)

				if config.Logging.Enabled {
					logger.Printf("Summary - Target: %s, fping Min: %.2f ms, fping Avg: %.2f ms, fping Max: %.2f ms",
						target, fpingMin, fpingAvg, fpingMax)
				}
			}
		}

		if err := scanner.Err(); err != nil {
			logger.Printf("Error reading from stderr: %v", err)
		}
	}()

	if err := cmd.Wait(); err != nil {
		logger.Printf("fping exited: %v", err)
	}

	// Flush any remaining writes
	writeAPI.Flush()
}
