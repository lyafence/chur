package keeper

import (
	"os"
	"testing"
	"time"
)

func keeperEnvCleanup() func() {
	keys := []string{
		"CHUR_KEEPER_LISTEN", "CHUR_KEEPER_HEALTH_LISTEN", "CHUR_KEEPER_TLS_MODE",
		"CHUR_KEEPER_TLS_CERT_PATH", "CHUR_KEEPER_TLS_KEY_PATH", "CHUR_KEEPER_CLIENT_CA_PATH",
		"CHUR_KEEPER_BACKEND", "CHUR_KEEPER_BACKEND_FS_ROOT", "CHUR_KEEPER_MAX_SECRET_SIZE",
		"CHUR_KEEPER_MAX_CONCURRENT", "CHUR_KEEPER_EXEC_COMMAND", "CHUR_KEEPER_EXEC_TIMEOUT",
		"CHUR_KEEPER_EXEC_MAX_STDOUT", "CHUR_KEEPER_HTTP_URL", "CHUR_KEEPER_HTTP_TOKEN_FILE",
		"CHUR_KEEPER_HTTP_TIMEOUT",
	}
	saved := make(map[string]string)
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok {
			saved[k] = v
		}
		os.Unsetenv(k)
	}
	return func() {
		for k, v := range saved {
			os.Setenv(k, v)
		}
	}
}

func TestConfigFromEnv_Defaults(t *testing.T) {
	defer keeperEnvCleanup()()
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv() unexpected error: %v", err)
	}
	if cfg.Listen != ":9443" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, ":9443")
	}
	if cfg.HealthListen != ":9444" {
		t.Errorf("HealthListen = %q, want %q", cfg.HealthListen, ":9444")
	}
	if cfg.TLSMode != TLSModeSelfSigned {
		t.Errorf("TLSMode = %q, want %q", cfg.TLSMode, TLSModeSelfSigned)
	}
	if cfg.MaxSecretSize != 1<<20 {
		t.Errorf("MaxSecretSize = %d, want %d", cfg.MaxSecretSize, 1<<20)
	}
	if cfg.MaxConcurrent != 100 {
		t.Errorf("MaxConcurrent = %d, want %d", cfg.MaxConcurrent, 100)
	}
	if cfg.ExecTimeout != 10*time.Second {
		t.Errorf("ExecTimeout = %v, want %v", cfg.ExecTimeout, 10*time.Second)
	}
	if cfg.ExecMaxStdout != 1<<20 {
		t.Errorf("ExecMaxStdout = %d, want %d", cfg.ExecMaxStdout, 1<<20)
	}
	if cfg.HTTPTimeout != 30*time.Second {
		t.Errorf("HTTPTimeout = %v, want %v", cfg.HTTPTimeout, 30*time.Second)
	}
	if cfg.BackendType != "filesystem" {
		t.Errorf("BackendType = %q, want %q", cfg.BackendType, "filesystem")
	}
}

func TestConfigFromEnv_Overrides(t *testing.T) {
	env := map[string]string{
		"CHUR_KEEPER_LISTEN":          ":9443",
		"CHUR_KEEPER_HEALTH_LISTEN":   ":9445",
		"CHUR_KEEPER_TLS_MODE":        "mtls",
		"CHUR_KEEPER_TLS_CERT_PATH":   "/custom/tls.crt",
		"CHUR_KEEPER_TLS_KEY_PATH":    "/custom/tls.key",
		"CHUR_KEEPER_CLIENT_CA_PATH":  "/custom/ca.crt",
		"CHUR_KEEPER_BACKEND":         "exec",
		"CHUR_KEEPER_BACKEND_FS_ROOT": "/custom/secrets",
		"CHUR_KEEPER_MAX_SECRET_SIZE": "2Mi",
		"CHUR_KEEPER_MAX_CONCURRENT":  "50",
		"CHUR_KEEPER_EXEC_COMMAND":    "/bin/get-secret",
		"CHUR_KEEPER_EXEC_TIMEOUT":    "5",
		"CHUR_KEEPER_EXEC_MAX_STDOUT": "512000",
		"CHUR_KEEPER_HTTP_URL":        "https://example.com",
		"CHUR_KEEPER_HTTP_TOKEN_FILE": "/token",
		"CHUR_KEEPER_HTTP_TIMEOUT":    "15",
	}
	for k, v := range env {
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv() unexpected error: %v", err)
	}
	if cfg.Listen != ":9443" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, ":9443")
	}
	if cfg.HealthListen != ":9445" {
		t.Errorf("HealthListen = %q, want %q", cfg.HealthListen, ":9445")
	}
	if cfg.TLSMode != TLSModeMTLS {
		t.Errorf("TLSMode = %q, want %q", cfg.TLSMode, TLSModeMTLS)
	}
	if cfg.TLSCertFile != "/custom/tls.crt" {
		t.Errorf("TLSCertFile = %q, want %q", cfg.TLSCertFile, "/custom/tls.crt")
	}
	if cfg.TLSKeyFile != "/custom/tls.key" {
		t.Errorf("TLSKeyFile = %q, want %q", cfg.TLSKeyFile, "/custom/tls.key")
	}
	if cfg.ClientCAFile != "/custom/ca.crt" {
		t.Errorf("ClientCAFile = %q, want %q", cfg.ClientCAFile, "/custom/ca.crt")
	}
	if cfg.BackendType != "exec" {
		t.Errorf("BackendType = %q, want %q", cfg.BackendType, "exec")
	}
	if cfg.FSRoot != "/custom/secrets" {
		t.Errorf("FSRoot = %q, want %q", cfg.FSRoot, "/custom/secrets")
	}
	if cfg.MaxSecretSize != 2<<20 {
		t.Errorf("MaxSecretSize = %d, want %d", cfg.MaxSecretSize, 2<<20)
	}
	if cfg.MaxConcurrent != 50 {
		t.Errorf("MaxConcurrent = %d, want %d", cfg.MaxConcurrent, 50)
	}
	if cfg.ExecCommand != "/bin/get-secret" {
		t.Errorf("ExecCommand = %q, want %q", cfg.ExecCommand, "/bin/get-secret")
	}
	if cfg.ExecTimeout != 5*time.Second {
		t.Errorf("ExecTimeout = %v, want %v", cfg.ExecTimeout, 5*time.Second)
	}
	if cfg.ExecMaxStdout != 512000 {
		t.Errorf("ExecMaxStdout = %d, want %d", cfg.ExecMaxStdout, 512000)
	}
	if cfg.HTTPURL != "https://example.com" {
		t.Errorf("HTTPURL = %q, want %q", cfg.HTTPURL, "https://example.com")
	}
	if cfg.HTTPTokenFile != "/token" {
		t.Errorf("HTTPTokenFile = %q, want %q", cfg.HTTPTokenFile, "/token")
	}
	if cfg.HTTPTimeout != 15*time.Second {
		t.Errorf("HTTPTimeout = %v, want %v", cfg.HTTPTimeout, 15*time.Second)
	}
}

func TestConfigFromEnv_TLSModeErrors(t *testing.T) {
	os.Setenv("CHUR_KEEPER_TLS_MODE", "invalid")
	defer os.Unsetenv("CHUR_KEEPER_TLS_MODE")

	_, err := ConfigFromEnv()
	if err == nil {
		t.Fatal("ConfigFromEnv() expected error for invalid TLS mode")
	}
}

func TestConfigFromEnv_MaxSecretSizeError(t *testing.T) {
	os.Setenv("CHUR_KEEPER_MAX_SECRET_SIZE", "invalid")
	defer os.Unsetenv("CHUR_KEEPER_MAX_SECRET_SIZE")

	_, err := ConfigFromEnv()
	if err == nil {
		t.Fatal("ConfigFromEnv() expected error for invalid max secret size")
	}
}

func TestConfigFromEnv_MaxConcurrentError(t *testing.T) {
	os.Setenv("CHUR_KEEPER_MAX_CONCURRENT", "0")
	defer os.Unsetenv("CHUR_KEEPER_MAX_CONCURRENT")

	_, err := ConfigFromEnv()
	if err == nil {
		t.Fatal("ConfigFromEnv() expected error for invalid max concurrent")
	}
}

func TestConfigFromEnv_ExecTimeoutError(t *testing.T) {
	os.Setenv("CHUR_KEEPER_EXEC_TIMEOUT", "not-a-number")
	defer os.Unsetenv("CHUR_KEEPER_EXEC_TIMEOUT")

	_, err := ConfigFromEnv()
	if err == nil {
		t.Fatal("ConfigFromEnv() expected error for invalid exec timeout")
	}
}

func TestConfigFromEnv_ExecMaxStdoutError(t *testing.T) {
	os.Setenv("CHUR_KEEPER_EXEC_MAX_STDOUT", "0")
	defer os.Unsetenv("CHUR_KEEPER_EXEC_MAX_STDOUT")

	_, err := ConfigFromEnv()
	if err == nil {
		t.Fatal("ConfigFromEnv() expected error for invalid exec max stdout")
	}
}

func TestConfigFromEnv_HTTPTimeoutError(t *testing.T) {
	os.Setenv("CHUR_KEEPER_HTTP_TIMEOUT", "-5")
	defer os.Unsetenv("CHUR_KEEPER_HTTP_TIMEOUT")

	_, err := ConfigFromEnv()
	if err == nil {
		t.Fatal("ConfigFromEnv() expected error for invalid http timeout")
	}
}
