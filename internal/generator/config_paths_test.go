package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedConfigPathPrecedenceAndAtomicSave(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("config-paths")
	outputDir := filepath.Join(t.TempDir(), "config-paths-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const runtimeTest = `package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func resetPathTestEnv(t *testing.T) (home, legacyPath string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{
		"CONFIG_PATHS_CONFIG",
		"CONFIG_PATHS_CONFIG_DIR",
		"CONFIG_PATHS_DATA_DIR",
		"CONFIG_PATHS_STATE_DIR",
		"CONFIG_PATHS_CACHE_DIR",
		"CONFIG_PATHS_HOME",
		"XDG_CONFIG_HOME",
		"XDG_DATA_HOME",
		"XDG_STATE_HOME",
		"XDG_CACHE_HOME",
		"MYAPI_TOKEN",
	} {
		t.Setenv(name, "")
	}
	return home, filepath.Join(home, ".config", "config-paths-pp-cli", "config.toml")
}

func TestDefaultConfigWriteUsesLegacyDefaultPath(t *testing.T) {
	_, legacyPath := resetPathTestEnv(t)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Path != legacyPath {
		t.Fatalf("Path = %q, want %q", cfg.Path, legacyPath)
	}
	if err := cfg.SaveCredential("secret"); err != nil {
		t.Fatalf("SaveCredential() error = %v", err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("config not written at default path: %v", err)
	}
}

func TestConfigFileEnvBeatsConfigDirEnv(t *testing.T) {
	home, _ := resetPathTestEnv(t)
	filePath := filepath.Join(home, "explicit", "config.toml")
	dirPath := filepath.Join(home, "config-dir")
	t.Setenv("CONFIG_PATHS_CONFIG", filePath)
	t.Setenv("CONFIG_PATHS_CONFIG_DIR", dirPath)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Path != filePath {
		t.Fatalf("Path = %q, want explicit file env %q", cfg.Path, filePath)
	}
	if err := cfg.SaveCredential("secret"); err != nil {
		t.Fatalf("SaveCredential() error = %v", err)
	}
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("explicit config file not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dirPath, "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("config dir env should not receive config when CONFIG file env is set: %v", err)
	}
}

func TestConfigKindPathFallsBackToLegacyReadAndReSavesResolvedPath(t *testing.T) {
	home, legacyPath := resetPathTestEnv(t)
	resolvedDir := filepath.Join(home, "xdg-config")
	t.Setenv("CONFIG_PATHS_CONFIG_DIR", resolvedDir)
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("base_url = \"https://legacy.example\"\ntoken = \"legacy-secret\"\n"), 0o600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	resolvedPath := filepath.Join(resolvedDir, "config.toml")
	if cfg.Path != resolvedPath {
		t.Fatalf("Path = %q, want resolved config-kind path %q", cfg.Path, resolvedPath)
	}
	if cfg.BaseURL != "https://legacy.example" || cfg.MyapiToken != "legacy-secret" {
		t.Fatalf("legacy config was not loaded: cfg=%+v", cfg)
	}
	if err := cfg.SaveCredential("new-secret"); err != nil {
		t.Fatalf("SaveCredential() error = %v", err)
	}
	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		t.Fatalf("read resolved config: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "https://legacy.example") || strings.Contains(text, "legacy-secret") || strings.Contains(text, "new-secret") || strings.Contains(text, "token") {
		t.Fatalf("resolved config did not preserve settings/exclude secrets:\n%s", text)
	}
}

func TestResolvedConfigWinsWhenLegacyAlsoExists(t *testing.T) {
	home, legacyPath := resetPathTestEnv(t)
	resolvedDir := filepath.Join(home, "xdg-config")
	resolvedPath := filepath.Join(resolvedDir, "config.toml")
	t.Setenv("CONFIG_PATHS_CONFIG_DIR", resolvedDir)
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(resolvedPath), 0o700); err != nil {
		t.Fatalf("mkdir resolved: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("base_url = \"https://legacy.example\"\n"), 0o600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	if err := os.WriteFile(resolvedPath, []byte("base_url = \"https://resolved.example\"\n"), 0o600); err != nil {
		t.Fatalf("write resolved: %v", err)
	}
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BaseURL != "https://resolved.example" {
		t.Fatalf("BaseURL = %q, want resolved config to win", cfg.BaseURL)
	}
}

func TestConfigSaveFailureLeavesOriginalParseable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission failure is POSIX-specific")
	}
	home, _ := resetPathTestEnv(t)
	configPath := filepath.Join(home, "atomic", "config.toml")
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("base_url = \"https://before.example\"\n"), 0o600); err != nil {
		t.Fatalf("write original: %v", err)
	}
	if err := os.Chmod(configDir, 0o500); err != nil {
		t.Fatalf("chmod config dir read-only: %v", err)
	}
	defer os.Chmod(configDir, 0o700)
	cfg := &Config{Path: configPath, BaseURL: "https://after.example"}
	if err := cfg.save(); err == nil {
		t.Fatalf("save() error = nil, want tmp write failure")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read original: %v", err)
	}
	if string(data) != "base_url = \"https://before.example\"\n" {
		t.Fatalf("original config changed after failed save:\n%s", string(data))
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "config", "config_paths_runtime_test.go"), []byte(runtimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/config", "-run", "Test.*Config")

	configSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	require.Contains(t, string(configSrc), "config-kind path")
	require.Contains(t, string(configSrc), "legacy config path")
}
