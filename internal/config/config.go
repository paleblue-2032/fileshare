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
	defaultQuotaBytes     = int64(100) << 30
	defaultMaxUploadBytes = int64(5) << 30
	defaultMinFreeBytes   = int64(3) << 30
	defaultRetentionDays  = 30
	defaultTrashDays      = 30
	defaultSweepMinutes   = 30
	defaultSessionDays    = 30
)

type Config struct {
	Addr            string
	DataDir         string
	PasswordHash    string
	DAVPasswordHash string
	QuotaBytes      int64
	MaxUploadBytes  int64
	MinFreeBytes    int64
	RetentionDays   int
	TrashDays       int
	SweepInterval   time.Duration
	WebDAV          bool
	TLSCert         string
	TLSKey          string
	SessionTTL      time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		Addr:            envString("FILESHARE_ADDR", "127.0.0.1:8087"),
		DataDir:         envString("FILESHARE_DATA_DIR", "/srv/fileshare-data"),
		PasswordHash:    envString("FILESHARE_PASSWORD_HASH", ""),
		DAVPasswordHash: envString("FILESHARE_DAV_PASSWORD_HASH", ""),
		QuotaBytes:      envInt64("FILESHARE_QUOTA_BYTES", defaultQuotaBytes),
		MaxUploadBytes:  envInt64("FILESHARE_MAX_UPLOAD_BYTES", defaultMaxUploadBytes),
		MinFreeBytes:    envInt64("FILESHARE_MIN_FREE_BYTES", defaultMinFreeBytes),
		RetentionDays:   envInt("FILESHARE_RETENTION_DAYS", defaultRetentionDays),
		TrashDays:       envInt("FILESHARE_TRASH_DAYS", defaultTrashDays),
		SweepInterval:   time.Duration(envInt("FILESHARE_SWEEP_MINUTES", defaultSweepMinutes)) * time.Minute,
		WebDAV:          envBool("FILESHARE_WEBDAV", true),
		TLSCert:         envString("FILESHARE_TLS_CERT", ""),
		TLSKey:          envString("FILESHARE_TLS_KEY", ""),
		SessionTTL:      time.Duration(defaultSessionDays) * 24 * time.Hour,
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Addr) == "" {
		return fmt.Errorf("FILESHARE_ADDR must not be empty")
	}
	if !filepath.IsAbs(c.DataDir) {
		return fmt.Errorf("FILESHARE_DATA_DIR must be an absolute path")
	}
	if c.PasswordHash == "" {
		return fmt.Errorf("FILESHARE_PASSWORD_HASH is required")
	}
	if c.QuotaBytes <= 0 {
		return fmt.Errorf("FILESHARE_QUOTA_BYTES must be positive")
	}
	if c.MaxUploadBytes <= 0 {
		return fmt.Errorf("FILESHARE_MAX_UPLOAD_BYTES must be positive")
	}
	if c.MinFreeBytes < 0 {
		return fmt.Errorf("FILESHARE_MIN_FREE_BYTES must not be negative")
	}
	if c.RetentionDays < 0 {
		return fmt.Errorf("FILESHARE_RETENTION_DAYS must not be negative")
	}
	if c.TrashDays < 0 {
		return fmt.Errorf("FILESHARE_TRASH_DAYS must not be negative")
	}
	if c.SweepInterval < time.Minute {
		return fmt.Errorf("FILESHARE_SWEEP_MINUTES must be at least 1")
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return fmt.Errorf("FILESHARE_TLS_CERT and FILESHARE_TLS_KEY must be set together")
	}
	return nil
}

func envString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func envInt64(key string, def int64) int64 {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return def
	}
	return n
}

func envInt(key string, def int) int {
	return int(envInt64(key, int64(def)))
}

func envBool(key string, def bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}
