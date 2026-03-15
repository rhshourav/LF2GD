// config/config.go
package config

import (
	"encoding/json"
	"os"
)

// Config struct fields must be exported (Capitalized) and
// match the keys in your config.json.
type Config struct {
	BaseURL            string `json:"base_url"`
	DownloadPath       string `json:"download_path"`
	ThreadsTotal       int    `json:"threads_total"`
	SegmentsPerFile    int    `json:"segments_per_file"`
	ConcurrentFiles    int    `json:"concurrent_files"`
	MinSegmentSizeMB   int    `json:"min_segment_size_mb"`
	RetryCount         int    `json:"retry_count"`
	HTTPTimeoutSeconds int    `json:"http_timeout_seconds"`
	UIRefreshMs        int    `json:"ui_refresh_ms"`
	TotalParts         int    `json:"total_parts"`
	DBPath             string `json:"db_path"`
}

// Load reads the local config.json file and returns a Config struct.
func Load() Config {
	file, err := os.Open("config.json")
	if err != nil {
		// Fallback to defaults or panic if config is missing
		panic("could not find config.json: " + err.Error())
	}
	defer file.Close()

	var cfg Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		panic("could not decode config.json: " + err.Error())
	}

	return cfg
}
