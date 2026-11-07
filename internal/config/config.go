package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMaxUploadMB = 2048
	defaultMaxFiles    = 20
	defaultMaxTextKB   = 64
	defaultExpiry      = 7 * 24 * time.Hour
	maxExpiry          = 365 * 24 * time.Hour
)

// StorageConfig describes where uploaded payloads are kept. The local driver
// is the default; the S3 driver speaks the S3-compatible protocol used by AWS
// S3, Cloudflare R2, Alibaba Cloud OSS, Tencent Cloud COS, Huawei Cloud OBS,
// Qiniu Kodo, Backblaze B2, MinIO and other compatible services.
type StorageConfig struct {
	Driver   string
	LocalDir string

	S3Provider        string
	S3Endpoint        string
	S3AccountID       string
	S3Region          string
	S3Bucket          string
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3SessionToken    string
	S3Prefix          string
	S3ForcePathStyle  *bool
}

// Config holds all runtime settings. Every value can be supplied through an
// environment variable; defaults are tuned for a self-hosted instance.
type Config struct {
	Addr          string
	DataDir       string
	DBPath        string
	PublicBaseURL string
	TrustProxy    bool
	AdminPassword string

	MaxUploadBytes int64
	MaxFiles       int
	MaxTextBytes   int
	DefaultExpiry  time.Duration
	MaxExpiry      time.Duration
	CleanupGrace   time.Duration

	Storage StorageConfig
}

// Load reads configuration from the environment.
func Load() (Config, error) {
	dataDir := envString("DATA_DIR", "./data")

	forcePathStyle, err := envOptionalBool("S3_FORCE_PATH_STYLE")
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Addr:           envString("ADDR", ":8080"),
		DataDir:        dataDir,
		DBPath:         envString("DB_PATH", filepath.Join(dataDir, "sendtome.db")),
		PublicBaseURL:  strings.TrimRight(envString("PUBLIC_BASE_URL", ""), "/"),
		TrustProxy:     envBool("TRUST_PROXY", false),
		AdminPassword:  envString("ADMIN_PASSWORD", ""),
		MaxUploadBytes: int64(envInt("MAX_UPLOAD_MB", defaultMaxUploadMB)) * 1024 * 1024,
		MaxFiles:       envInt("MAX_FILES", defaultMaxFiles),
		MaxTextBytes:   envInt("MAX_TEXT_KB", defaultMaxTextKB) * 1024,
		DefaultExpiry:  defaultExpiry,
		MaxExpiry:      maxExpiry,
		CleanupGrace:   time.Duration(envInt("CLEANUP_GRACE_HOURS", 24)) * time.Hour,
		Storage: StorageConfig{
			Driver:            strings.ToLower(envString("STORAGE_DRIVER", "local")),
			LocalDir:          envString("LOCAL_STORAGE_DIR", filepath.Join(dataDir, "files")),
			S3Provider:        strings.ToLower(envString("S3_PROVIDER", "aws")),
			S3Endpoint:        strings.TrimRight(envString("S3_ENDPOINT", ""), "/"),
			S3AccountID:       envString("S3_ACCOUNT_ID", ""),
			S3Region:          envString("S3_REGION", ""),
			S3Bucket:          envString("S3_BUCKET", ""),
			S3AccessKeyID:     envString("S3_ACCESS_KEY_ID", ""),
			S3SecretAccessKey: envString("S3_SECRET_ACCESS_KEY", ""),
			S3SessionToken:    envString("S3_SESSION_TOKEN", ""),
			S3Prefix:          envString("S3_PREFIX", "send-to-me"),
			S3ForcePathStyle:  forcePathStyle,
		},
	}

	if cfg.MaxUploadBytes <= 0 {
		return Config{}, fmt.Errorf("MAX_UPLOAD_MB must be greater than zero")
	}
	if cfg.MaxFiles <= 0 {
		return Config{}, fmt.Errorf("MAX_FILES must be greater than zero")
	}
	if cfg.MaxTextBytes <= 0 {
		return Config{}, fmt.Errorf("MAX_TEXT_KB must be greater than zero")
	}
	if cfg.CleanupGrace < 0 {
		return Config{}, fmt.Errorf("CLEANUP_GRACE_HOURS must not be negative")
	}
	if cfg.Storage.Driver != "local" && cfg.Storage.Driver != "s3" {
		return Config{}, fmt.Errorf("STORAGE_DRIVER must be either local or s3")
	}

	return cfg, nil
}

func envString(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func envInt(key string, fallback int) int {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return value
}

func envOptionalBool(key string) (*bool, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("%s must be true or false", key)
	}
	return &value, nil
}
