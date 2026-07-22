package config

import (
	"os"
	"strings"
)

type Config struct {
	MongoURI         string
	PostgresURI      string
	UnitedProfileDir string
	UnitedPassword   string
	Headless         bool
}

func Load() Config {
	loadDotEnv(".env")

	return Config{
		MongoURI:         getEnv("MONGO_URI", "mongodb://localhost:27017"),
		PostgresURI:      getEnv("POSTGRES_URI", "postgres://cloudmilesscouter:cloudmilesscouter@localhost:5432/cloudmilesscouter?sslmode=disable"),
		UnitedProfileDir: getEnv("UNITED_PROFILE_DIR", ".united-profile"),
		UnitedPassword:   getEnv("UNITED_PASSWORD", ""),
		Headless:         getEnv("HEADLESS", "false") == "true",
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// loadDotEnv sets environment variables from a simple KEY=VALUE file, one per
// line, without overriding variables already set in the real environment.
func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		os.Setenv(key, strings.TrimSpace(value))
	}
}
