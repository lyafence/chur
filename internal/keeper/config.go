package keeper

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/lyafence/chur/internal/keeper/bytesize"
)

type TLSMode string

const (
	TLSModeSelfSigned TLSMode = "self-signed"
	TLSModeMTLS       TLSMode = "mtls"
)

type Config struct {
	Listen        string
	HealthListen  string
	TLSMode       TLSMode
	TLSCertFile   string
	TLSKeyFile    string
	ClientCAFile  string
	BackendType   string
	Backend       Backend
	MaxSecretSize int64
	MaxConcurrent int
	ExecCommand   string
	ExecTimeout   time.Duration
	ExecMaxStdout int64
}

func DefaultConfig() *Config {
	return &Config{
		Listen:        ":9443",
		HealthListen:  ":9444",
		TLSMode:       TLSModeSelfSigned,
		MaxSecretSize: 1 << 20,
		MaxConcurrent: 100,
		ExecTimeout:   10 * time.Second,
		ExecMaxStdout: 1 << 20,
	}
}

func ConfigFromEnv() (*Config, error) {
	cfg := DefaultConfig()
	if v := os.Getenv("CHUR_KEEPER_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("CHUR_KEEPER_HEALTH_LISTEN"); v != "" {
		cfg.HealthListen = v
	}
	switch v := os.Getenv("CHUR_KEEPER_TLS_MODE"); v {
	case "mtls":
		cfg.TLSMode = TLSModeMTLS
	case "self-signed", "":
		cfg.TLSMode = TLSModeSelfSigned
	}
	if v := os.Getenv("CHUR_KEEPER_TLS_CERT"); v != "" {
		cfg.TLSCertFile = v
	}
	if v := os.Getenv("CHUR_KEEPER_TLS_KEY"); v != "" {
		cfg.TLSKeyFile = v
	}
	if v := os.Getenv("CHUR_KEEPER_TLS_CLIENT_CA"); v != "" {
		cfg.ClientCAFile = v
	}
	if v := os.Getenv("CHUR_KEEPER_BACKEND"); v != "" {
		cfg.BackendType = v
	} else {
		cfg.BackendType = "filesystem"
	}
	if v := os.Getenv("CHUR_KEEPER_MAX_SECRET_SIZE"); v != "" {
		n, err := bytesize.Parse(v)
		if err != nil {
			return nil, fmt.Errorf("invalid CHUR_KEEPER_MAX_SECRET_SIZE %q: %w", v, err)
		}
		cfg.MaxSecretSize = n
	}
	if v := os.Getenv("CHUR_KEEPER_MAX_CONCURRENT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid CHUR_KEEPER_MAX_CONCURRENT %q: %w", v, err)
		}
		cfg.MaxConcurrent = n
	}
	if v := os.Getenv("CHUR_KEEPER_EXEC_COMMAND"); v != "" {
		cfg.ExecCommand = v
	}
	if v := os.Getenv("CHUR_KEEPER_EXEC_TIMEOUT"); v != "" {
		if d, err := strconv.Atoi(v); err == nil && d > 0 {
			cfg.ExecTimeout = time.Duration(d) * time.Second
		}
	}
	if v := os.Getenv("CHUR_KEEPER_EXEC_MAX_STDOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.ExecMaxStdout = int64(n)
		}
	}
	return cfg, nil
}
