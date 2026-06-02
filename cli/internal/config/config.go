package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	configDirPermission  = 0o700
	configFilePermission = 0o600
)

// DefaultGCRetentionDays is the product-specified default trash retention
// window used by `pc gc` when gc_retention_days is not set in config.
const DefaultGCRetentionDays = 30

var (
	chmodFileFn = func(f *os.File, mode os.FileMode) error { return f.Chmod(mode) }
	syncFileFn  = func(f *os.File) error { return f.Sync() }
	closeFileFn = func(f *os.File) error { return f.Close() }
)

// Mode identifies whether the config is local-only or cloud-enabled.
type Mode string

const (
	ModeLocalOnly Mode = "local-only"
	ModeCloud     Mode = "cloud"
)

// Config stores local/cloud runtime configuration.
type Config struct {
	NeonURL          string `json:"neon_url,omitempty"`
	S3Bucket         string `json:"s3_bucket,omitempty"`
	S3Region         string `json:"s3_region,omitempty"`
	AWSProfile       string `json:"aws_profile,omitempty"`
	ActiveProject    string `json:"active_project,omitempty"`
	S3Endpoint       string `json:"s3_endpoint,omitempty"`
	S3ForcePathStyle bool   `json:"s3_force_path_style,omitempty"`
	APIKey           string `json:"api_key,omitempty"`
	// GCRetentionDays is the trash retention window in days used by `pc gc`.
	// A pointer distinguishes "unset" (use DefaultGCRetentionDays) from an
	// explicit value, avoiding a silent zero-day default for legacy configs.
	GCRetentionDays *int `json:"gc_retention_days,omitempty"`
}

// Store reads and writes config under ~/personal-context/.pc/config.json.
type Store struct {
	homeDir string
}

// NewStore creates a config store rooted at homeDir.
// Args: homeDir is the user home directory path.
// Returns: a config store or an error when homeDir is empty.
func NewStore(homeDir string) (Store, error) {
	if strings.TrimSpace(homeDir) == "" {
		return Store{}, fmt.Errorf("home directory is required")
	}
	return Store{homeDir: homeDir}, nil
}

// Path returns the absolute config file path.
// Args: none.
// Returns: ~/personal-context/.pc/config.json rooted at the store home dir.
func (s Store) Path() string {
	return filepath.Join(s.homeDir, "personal-context", ".pc", "config.json")
}

// Read loads configuration from disk.
// Args: none.
// Returns: parsed config or an error for missing/corrupt/invalid config.
func (s Store) Read() (Config, error) {
	path := s.Path()
	bytes, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	if err := json.Unmarshal(bytes, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}

	if _, err := cfg.Mode(); err != nil {
		return Config{}, err
	}

	if err := ValidateGCRetentionDays(cfg.GCRetentionDays); err != nil {
		return Config{}, fmt.Errorf("gc_retention_days: %w", err)
	}

	return cfg, nil
}

// GCRetention returns the effective trash retention window as a duration.
// Args: none.
// Returns: the configured retention when gc_retention_days is set, otherwise
// DefaultGCRetentionDays. Assumes the value was validated by Read/Write.
func (c Config) GCRetention() time.Duration {
	days := DefaultGCRetentionDays
	if c.GCRetentionDays != nil {
		days = *c.GCRetentionDays
	}
	return time.Duration(days) * 24 * time.Hour
}

// Write persists configuration using 0600 permissions.
// Args: cfg is the desired configuration.
// Returns: nil on success or an error for invalid config or write failures.
func (s Store) Write(cfg Config) error {
	if _, err := cfg.Mode(); err != nil {
		return err
	}
	if err := ValidateGCRetentionDays(cfg.GCRetentionDays); err != nil {
		return fmt.Errorf("gc_retention_days: %w", err)
	}

	path := s.Path()
	if err := os.MkdirAll(filepath.Dir(path), configDirPermission); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	content, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	content = append(content, '\n')

	if err := writeFileAtomically(path, content, configFilePermission); err != nil {
		return err
	}

	return nil
}

// writeFileAtomically writes content to a temp file in the target directory
// and renames it into place to guarantee all-or-nothing replacement.
func writeFileAtomically(path string, content []byte, permission os.FileMode) error {
	dir := filepath.Dir(path)
	tempFile, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config file in %s: %w", dir, err)
	}
	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if err := chmodFileFn(tempFile, permission); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("set temp config permissions %s: %w", tempPath, err)
	}
	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write temp config %s: %w", tempPath, err)
	}
	if err := syncFileFn(tempFile); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("sync temp config %s: %w", tempPath, err)
	}
	if err := closeFileFn(tempFile); err != nil {
		return fmt.Errorf("close temp config %s: %w", tempPath, err)
	}

	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace config %s: %w", path, err)
	}
	cleanupTemp = false
	return nil
}

// Mode resolves the configuration mode and validates cloud completeness.
// Args: none.
// Returns: ModeLocalOnly when all cloud fields are empty; ModeCloud when core cloud fields are set; error on partial cloud config.
// Backward compatibility: api_key is optional in Mode detection so legacy cloud configs remain readable for remediation flows.
func (c Config) Mode() (Mode, error) {
	coreValues := []string{c.NeonURL, c.S3Bucket, c.S3Region, c.AWSProfile}
	coreSetCount := 0
	for _, value := range coreValues {
		if strings.TrimSpace(value) != "" {
			coreSetCount++
		}
	}
	apiKeySet := strings.TrimSpace(c.APIKey) != ""

	if coreSetCount == 0 && !apiKeySet {
		return ModeLocalOnly, nil
	}
	if coreSetCount == len(coreValues) {
		return ModeCloud, nil
	}

	if coreSetCount == 0 && apiKeySet {
		return "", errors.New("invalid config: api_key requires cloud fields neon_url, s3_bucket, s3_region, and aws_profile")
	}

	return "", errors.New("invalid config: cloud mode requires neon_url, s3_bucket, s3_region, and aws_profile")
}
