package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the complete application configuration.
// All fields are loaded from environment variables.
// Use Load() to construct a validated Config instance.
type Config struct {
	HTTP   HTTPConfig
	DB     DBConfig
	Redis  RedisConfig
	GitHub GitHubConfig
	SMTP   SMTPConfig
	App    AppConfig
}

// HTTPConfig contains settings for the HTTP server.
type HTTPConfig struct {
	// Port to listen on (default: 8080)
	Port int
	// ReadTimeout for incoming requests
	ReadTimeout time.Duration
	// WriteTimeout for outgoing responses
	WriteTimeout time.Duration
	// IdleTimeout for keep-alive connections
	IdleTimeout time.Duration
}

// DBConfig contains PostgreSQL connection settings.
type DBConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
	// MaxOpenConns limits number of open connections in the pool
	MaxOpenConns int
	// MaxIdleConns limits number of idle connections in the pool
	MaxIdleConns int
	// ConnMaxLifetime limits maximum lifetime of a connection
	ConnMaxLifetime time.Duration
}

// DSN returns a PostgreSQL connection string suitable for database/sql.
func (c DBConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode,
	)
}

// RedisConfig contains Redis connection settings.
type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
	// CacheTTL is the TTL for cached GitHub API responses (default: 10 minutes)
	CacheTTL time.Duration
}

// Addr returns "host:port" string for Redis client.
func (c RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// GitHubConfig holds GitHub API settings.
type GitHubConfig struct {
	// Token is a Personal Access Token for authenticated requests
	// (5000 req/hour vs 60 req/hour for anonymous)
	Token string
	// ScanInterval defines how often the scanner polls GitHub for new releases
	ScanInterval time.Duration
}

// SMTPConfig holds email delivery settings.
type SMTPConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	// From is the "From" address used in sent emails
	From string
	// UseTLS forces STARTTLS. False is fine for local Mailpit.
	UseTLS bool
}

// AppConfig contains general application settings.
type AppConfig struct {
	// BaseURL is the public-facing URL, used for building confirmation links
	BaseURL string
	// MigrationPath is the path to migration files (default: "migrations")
	MigrationPath string
	// LogLevel controls log verbosity: debug | info | warn | error
	LogLevel string
	// Env is the runtime environment: development | production
	Env string
}

// Load reads all configuration from environment variables.
// It returns an error if any required variable is missing or invalid.
// Optional variables fall back to sensible defaults.
func Load() (*Config, error) {
	var errs []string

	cfg := &Config{}

	// --- HTTP ---
	cfg.HTTP.Port = getEnvInt("HTTP_PORT", 8080)
	cfg.HTTP.ReadTimeout = getEnvDuration("HTTP_READ_TIMEOUT", 10*time.Second)
	cfg.HTTP.WriteTimeout = getEnvDuration("HTTP_WRITE_TIMEOUT", 30*time.Second)
	cfg.HTTP.IdleTimeout = getEnvDuration("HTTP_IDLE_TIMEOUT", 60*time.Second)

	// --- Database ---
	cfg.DB.Host = getEnvRequired("POSTGRES_HOST", &errs)
	cfg.DB.Port = getEnvInt("POSTGRES_PORT", 5432)
	cfg.DB.User = getEnvRequired("POSTGRES_USER", &errs)
	cfg.DB.Password = getEnvRequired("POSTGRES_PASSWORD", &errs)
	cfg.DB.Name = getEnvRequired("POSTGRES_DB", &errs)
	cfg.DB.SSLMode = getEnvDefault("POSTGRES_SSLMODE", "disable")
	cfg.DB.MaxOpenConns = getEnvInt("POSTGRES_MAX_OPEN_CONNS", 25)
	cfg.DB.MaxIdleConns = getEnvInt("POSTGRES_MAX_IDLE_CONNS", 10)
	cfg.DB.ConnMaxLifetime = getEnvDuration("POSTGRES_CONN_MAX_LIFETIME", 5*time.Minute)

	// --- Redis ---
	cfg.Redis.Host = getEnvDefault("REDIS_HOST", "localhost")
	cfg.Redis.Port = getEnvInt("REDIS_PORT", 6379)
	cfg.Redis.Password = getEnvDefault("REDIS_PASSWORD", "")
	cfg.Redis.DB = getEnvInt("REDIS_DB", 0)
	cfg.Redis.CacheTTL = getEnvDuration("REDIS_CACHE_TTL", 10*time.Minute)

	// --- GitHub ---
	// Token is optional but strongly recommended to avoid rate limiting
	cfg.GitHub.Token = getEnvDefault("GITHUB_KEY", "")
	cfg.GitHub.ScanInterval = getEnvDuration("GITHUB_SCAN_INTERVAL", 5*time.Minute)

	// --- SMTP ---
	cfg.SMTP.Host = getEnvDefault("SMTP_HOST", "localhost")
	cfg.SMTP.Port = getEnvInt("SMTP_PORT", 1025)
	cfg.SMTP.User = getEnvDefault("SMTP_USER", "")
	cfg.SMTP.Password = getEnvDefault("SMTP_PASSWORD", "")
	cfg.SMTP.From = getEnvDefault("SMTP_FROM", "noreply@github-notify.local")
	cfg.SMTP.UseTLS = getEnvBool("SMTP_USE_TLS", false)

	// --- App ---
	cfg.App.BaseURL = getEnvDefault("APP_BASE_URL", "http://localhost:8080")
	cfg.App.MigrationPath = getEnvDefault("MIGRATION_PATH", "migrations")
	cfg.App.LogLevel = getEnvDefault("LOG_LEVEL", "info")
	cfg.App.Env = getEnvDefault("APP_ENV", "development")

	if len(errs) > 0 {
		return nil, fmt.Errorf("config: missing required environment variables: %s", strings.Join(errs, ", "))
	}

	return cfg, nil
}

// IsDevelopment returns true when running in development mode.
func (c *Config) IsDevelopment() bool {
	return c.App.Env == "development"
}

// IsProduction returns true when running in production mode.
func (c *Config) IsProduction() bool {
	return c.App.Env == "production"
}

// --------------------------------------------------------------------------
// Helper functions
// --------------------------------------------------------------------------

// getEnvRequired reads an env var and appends its name to errs if missing.
func getEnvRequired(key string, errs *[]string) string {
	v := os.Getenv(key)
	if v == "" {
		*errs = append(*errs, key)
	}
	return v
}

// getEnvDefault reads an env var and returns fallback if not set.
func getEnvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnvInt reads an env var as int, returning fallback on parse failure.
func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// getEnvBool reads an env var as bool, returning fallback on parse failure.
func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

// getEnvDuration reads an env var as time.Duration, returning fallback on failure.
// Format: "30s", "5m", "1h" etc.
func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
