package ed2ksrv

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	storageBackendJSON  = "json"
	storageBackendMySQL = "mysql"
	storageBackendPgSQL = "pgsql"
)

// catalogStore persists and loads the static catalog from a backing store.
type catalogStore interface {
	Load() ([]FileRecord, error)
	Save([]FileRecord) error
	Description() string
	Close() error
}

// newCatalogStore selects the configured persistence backend.
func newCatalogStore(cfg Config) (catalogStore, error) {
	backend := strings.ToLower(strings.TrimSpace(cfg.StorageBackend))
	if backend == "" || backend == storageBackendJSON {
		if strings.TrimSpace(cfg.CatalogPath) == "" {
			return nil, fmt.Errorf("catalog_path is required when storage_backend is json")
		}
		return &jsonCatalogStore{path: cfg.CatalogPath}, nil
	}
	if backend != storageBackendMySQL && backend != storageBackendPgSQL {
		return nil, fmt.Errorf("unsupported storage_backend %q", cfg.StorageBackend)
	}
	if strings.TrimSpace(cfg.DatabaseDSN) == "" {
		return nil, fmt.Errorf("database_dsn is required when storage_backend is %s", backend)
	}
	tableName := strings.TrimSpace(cfg.DatabaseTable)
	if tableName == "" {
		tableName = "shared_files"
	}
	driverName := backend
	if backend == storageBackendPgSQL {
		driverName = "pgx"
	}
	db, err := sql.Open(driverName, cfg.DatabaseDSN)
	if err != nil {
		return nil, err
	}
	store := &sqlCatalogStore{db: db, backend: backend, tableName: tableName}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// jsonCatalogStore persists the catalog in the original JSON file format.
type jsonCatalogStore struct {
	path string
}

func (s *jsonCatalogStore) Load() ([]FileRecord, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []FileRecord{}, nil
		}
		return nil, err
	}
	var disk catalogDisk
	if err := json.Unmarshal(data, &disk); err != nil {
		return nil, err
	}
	return disk.Files, nil
}

func (s *jsonCatalogStore) Save(files []FileRecord) error {
	if strings.TrimSpace(s.path) == "" {
		return fmt.Errorf("catalog path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(catalogDisk{Files: cloneFiles(files)}, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return os.WriteFile(s.path, payload, 0o644)
}

func (s *jsonCatalogStore) Description() string {
	return s.path
}

func (s *jsonCatalogStore) Close() error {
	return nil
}
