package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalListenerRejectsLANBinding(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8766", "192.168.1.15:8766", "localhost:8766"} {
		cfg := Default()
		cfg.Server.LocalAddr = addr
		if err := cfg.Validate(); err == nil {
			t.Fatalf("unsafe local listener accepted: %s", addr)
		}
	}
	cfg := Default()
	cfg.Server.LocalAddr = "127.0.0.1:8766"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadResolvesStorageRelativeToConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "yui.config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"data_dir":"./data","database":{"driver":"sqlite","path":"./data/yui.db"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "data"); cfg.DataDir != want {
		t.Fatalf("data_dir=%q want %q", cfg.DataDir, want)
	}
	if want := filepath.Join(dir, "data", "yui.db"); cfg.Database.Path != want {
		t.Fatalf("database.path=%q want %q", cfg.Database.Path, want)
	}
	if want := filepath.Join(dir, "data", "yui.key"); cfg.Crypto.KeyFile != want {
		t.Fatalf("crypto.key_file=%q want %q", cfg.Crypto.KeyFile, want)
	}
}
