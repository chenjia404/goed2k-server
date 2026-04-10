package ed2ksrv

import (
	"path/filepath"
	"testing"
)

func TestLoadConfigMissingFileUsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-config.json")
	cfg, usedDefaults, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !usedDefaults {
		t.Fatalf("expected usedDefaults true")
	}
	if cfg.ListenAddress != defaultListenAddress {
		t.Fatalf("ListenAddress: %q", cfg.ListenAddress)
	}
	if cfg.CatalogPath != defaultCatalogPath {
		t.Fatalf("CatalogPath: %q", cfg.CatalogPath)
	}
	if cfg.StorageBackend != storageBackendJSON {
		t.Fatalf("StorageBackend: %s", cfg.StorageBackend)
	}
}
