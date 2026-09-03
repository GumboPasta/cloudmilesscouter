package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	MongoURI           string
	PostgresURI        string
	UnitedProfileDir   string
	UnitedUsername     string
	UnitedPassword     string
	GmailAddress       string // for AutoBootstrap: inbox to poll for United's OTP email
	GmailAppPassword   string // Gmail App Password — myaccount.google.com/apppasswords, NOT the account password
	AmericanProfileDir string
	DeltaProfileDir    string
	AlaskaProfileDir   string
	Headless           bool
	KafkaBrokers       string
	KafkaGroupID       string
	RedisAddr          string // host:port for the /api/search read cache (Phase 4)
	WorkerCount        int
	MaxAttempts        int           // total scrape tries per job before it is dropped
	RetryBackoffBase   time.Duration // first retry waits this long; doubles each attempt (capped in the worker)

	APIPort            string   // port cmd/api listens on
	CORSAllowedOrigins []string // exact origins allowed to call the API from a browser
	RateLimitPerMinute int      // per-client-IP request budget per minute; 0 disables the limiter
}

// Load reads .env from the current working directory (not the executable's
// directory or the repo root), then falls back to defaults. The profile-dir
// defaults below are relative too, so run the binaries from the repo root or
// set the vars explicitly — see the "Running the binaries" note in README.md.
func Load() Config {
	loadDotEnv(".env")

	return Config{
		MongoURI:           getEnv("MONGO_URI", "mongodb://localhost:27017"),
		PostgresURI:        getEnv("POSTGRES_URI", "postgres://cloudmilesscouter:cloudmilesscouter@localhost:5432/cloudmilesscouter?sslmode=disable"),
		UnitedProfileDir:   getEnv("UNITED_PROFILE_DIR", ".united-profile"),
		UnitedUsername:     getEnv("UNITED_USERNAME", ""),
		UnitedPassword:     getEnv("UNITED_PASSWORD", ""),
		GmailAddress:       getEnv("GMAIL_ADDRESS", ""),
		GmailAppPassword:   getEnv("GMAIL_APP_PASSWORD", ""),
		AmericanProfileDir: getEnv("AMERICAN_PROFILE_DIR", ".american-profile"),
		DeltaProfileDir:    getEnv("DELTA_PROFILE_DIR", ".delta-profile"),
		AlaskaProfileDir:   getEnv("ALASKA_PROFILE_DIR", ".alaska-profile"),
		Headless:           getEnv("HEADLESS", "false") == "true",
		KafkaBrokers:       getEnv("KAFKA_BROKERS", "localhost:9092"),
		KafkaGroupID:       getEnv("KAFKA_GROUP_ID", "scrape-workers"),
		RedisAddr:          getEnv("REDIS_ADDR", "localhost:6379"),
		WorkerCount:        getEnvInt("WORKER_COUNT", 5),
		MaxAttempts:        getEnvInt("MAX_SCRAPE_ATTEMPTS", 3),
		RetryBackoffBase:   getEnvDuration("RETRY_BACKOFF_BASE", 2*time.Second),

		APIPort:            getEnv("API_PORT", "8080"),
		CORSAllowedOrigins: getEnvList("CORS_ALLOWED_ORIGINS", []string{"http://localhost:5173"}),
		RateLimitPerMinute: getEnvInt("RATE_LIMIT_PER_MINUTE", 120),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// getEnvList reads a comma-separated variable into a slice, trimming spaces and
// dropping empty entries. An unset or all-empty value yields the fallback.
func getEnvList(key string, fallback []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
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
