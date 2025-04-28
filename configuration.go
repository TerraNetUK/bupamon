package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"

	yaml "gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	BupaMon struct {
		WindowSizes []string `yaml:"window_sizes"`
		SourceHost  string   `yaml:"source_host"`
	} `yaml:"bupamon"`

	Logging struct {
		Enabled bool   `yaml:"enabled"`
		Logfile string `yaml:"logfile"`
	} `yaml:"logging"`

	InfluxDB struct {
		Host        string `yaml:"host"`
		Port        int    `yaml:"port"`
		Org         string `yaml:"org"`
		Bucket      string `yaml:"bucket"`
		Measurement string `yaml:"measurement"`
		Token       string `yaml:"token"`
	} `yaml:"influxdb"`

	FPing struct {
		Path string   `yaml:"path"`
		Args []string `yaml:"args"`
	} `yaml:"fping"`

	Targets struct {
		Hosts []string `yaml:"hosts"`
		File  string   `yaml:"file"`
	} `yaml:"targets"`
}

// ParseDuration parses a human-readable duration string
func ParseDuration(s string) (time.Duration, error) {
	// Handle hour notation specially
	if strings.HasSuffix(s, "hr") {
		h, err := strconv.Atoi(strings.TrimSuffix(s, "hr"))
		if err != nil {
			return 0, err
		}
		return time.Duration(h) * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// LoadConfig loads the application configuration from a YAML file
func LoadConfig(filepath string) (*Config, error) {
	var data, err = os.ReadFile(filepath)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	// Set default values if not specified
	if config.BupaMon.SourceHost == "" {
		hostname, err := os.Hostname()
		if err == nil {
			config.BupaMon.SourceHost = hostname
		} else {
			config.BupaMon.SourceHost = "unknown"
		}
	}

	return &config, nil
}

// LoadTargetsFromFile loads additional targets from a text file
func LoadTargetsFromFile(filepath string) ([]string, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer func(file *os.File) ([]string, error) {
		err := file.Close()
		if err != nil {
			return nil, err
		}
		return nil, nil
	}(file)

	var targets []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			targets = append(targets, line)
		}
	}

	return targets, scanner.Err()
}
