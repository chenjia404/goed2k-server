package ed2ksrv

import (
	"path/filepath"
	"testing"
)

func TestJSONCatalogStoreLoadMissingFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-catalog.json")
	store := &jsonCatalogStore{path: path}
	files, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no files, got %d", len(files))
	}
}

func TestLoadCatalogFromConfigMissingJSONFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-catalog.json")
	cfg := DefaultConfig()
	cfg.CatalogPath = path
	cfg.StorageBackend = storageBackendJSON
	cat, err := LoadCatalogFromConfig(cfg)
	if err != nil {
		t.Fatalf("LoadCatalogFromConfig: %v", err)
	}
	defer func() { _ = cat.Close() }()
	if cat.Count() != 0 {
		t.Fatalf("expected empty catalog, count=%d", cat.Count())
	}
}
