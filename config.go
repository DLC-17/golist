package main

import (
	"bufio"
	"os"
	"strings"
)

type Config struct {
	GeminiAPIKey     string
	GeminiModel      string
	EbayClientID     string
	EbayClientSecret string
	EbayRefreshToken string
	EbayEnv          string // "sandbox" or "production"
	DataDir          string // Base folder for dump/processing/drafts/published
}

// LoadConfig loads configuration from environment variables and optional .env file.
func LoadConfig() Config {
	loadDotEnv(".env")

	cfg := Config{
		GeminiAPIKey:     getEnv("GEMINI_API_KEY", ""),
		GeminiModel:      getEnv("GEMINI_MODEL", "gemini-2.5-flash"),
		EbayClientID:     getEnv("EBAY_CLIENT_ID", ""),
		EbayClientSecret: getEnv("EBAY_CLIENT_SECRET", ""),
		EbayRefreshToken: getEnv("EBAY_REFRESH_TOKEN", ""),
		EbayEnv:          getEnv("EBAY_ENV", "sandbox"),
		DataDir:          getEnv("DATA_DIR", "./data"),
	}

	return cfg
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// loadDotEnv reads a simple key=value .env file without external dependencies.
func loadDotEnv(filepath string) {
	f, err := os.Open(filepath)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
			if os.Getenv(k) == "" {
				_ = os.Setenv(k, v)
			}
		}
	}
}
