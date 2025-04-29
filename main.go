package main

import (
	"crypto/tls"
	"fmt"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// Load configuration
	config, err := LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	// Default values if not in config
	if config.FPing.RestartThreshold == 0 {
		config.FPing.RestartThreshold = 30 // 30% packet loss
	}
	if config.FPing.ConsecutiveThreshold == 0 {
		config.FPing.ConsecutiveThreshold = 3 // 3 consecutive high loss readings
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

	// Set up InfluxDB client with TLS verification disabled
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // Disable certificate verification
			},
		},
	}

	// Create InfluxDB client options
	options := influxdb2.DefaultOptions()
	options.SetBatchSize(20)          // Reduce batch size
	options.SetRetryInterval(5000)    // Retry every 5 seconds
	options.SetMaxRetries(5)          // Retry 5 times
	options.SetLogLevel(3)            // More verbose logging
	options.SetHTTPClient(httpClient) // Use our custom HTTP client

	influxURL := fmt.Sprintf("https://%s:%d", config.InfluxDB.Host, config.InfluxDB.Port)
	client := influxdb2.NewClientWithOptions(influxURL, config.InfluxDB.Token, options)
	defer client.Close()

	// Create process manager
	processManager := NewProcessManager(config, logger, client)

	// Set up signal handling for graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		logger.Println("Received shutdown signal, stopping fping and flushing writes...")
		processManager.Stop()
		client.Close()
		os.Exit(0)
	}()

	// Start the process
	if err := processManager.Start(); err != nil {
		logger.Fatalf("Failed to start fping: %v", err)
	}

	// Wait indefinitely
	select {}
}
