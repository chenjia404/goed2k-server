package ed2ksrv

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	defaultListenAddress      = ":4661"
	defaultAdminListenAddress = ":8080"
	defaultServerName         = "goed2k-server"
	defaultDescription        = "Minimal eD2k/eMule compatible server"
	defaultBatchSize          = 100000
)

// Config describes the runtime settings for the ED2K server.
type Config struct {
	ListenAddress      string `json:"listen_address"`
	AdminListenAddress string `json:"admin_listen_address"`
	AdminToken         string `json:"admin_token"`
	ServerName         string `json:"server_name"`
	ServerDescription  string `json:"server_description"`
	Message            string `json:"message"`
	StorageBackend     string `json:"storage_backend"`
	CatalogPath        string `json:"catalog_path"`
	DatabaseDSN        string `json:"database_dsn"`
	DatabaseTable      string `json:"database_table"`
	SearchBatchSize    int    `json:"search_batch_size"`
	TCPFlags           int32  `json:"tcp_flags"`
	AuxPort            int32  `json:"aux_port"`
	// ProtocolObfuscation enables eMule-style TCP obfuscation (DH + RC4) on the ED2K listener when the client starts with a non-ED2K first byte.
	ProtocolObfuscation bool `json:"protocol_obfuscation"`
}

// DefaultConfig returns a working baseline configuration.
func DefaultConfig() Config {
	return Config{
		ListenAddress:      defaultListenAddress,
		AdminListenAddress: defaultAdminListenAddress,
		ServerName:         defaultServerName,
		ServerDescription:  defaultDescription,
		Message:            "Welcome to goed2k-server",
		StorageBackend:     storageBackendJSON,
		SearchBatchSize:       defaultBatchSize,
		ProtocolObfuscation:   true,
	}
}

// Normalize applies defaults and validates required fields.
func (c Config) Normalize() (Config, error) {
	if c.ListenAddress == "" {
		c.ListenAddress = defaultListenAddress
	}
	if c.AdminListenAddress == "" {
		c.AdminListenAddress = defaultAdminListenAddress
	}
	if c.ServerName == "" {
		c.ServerName = defaultServerName
	}
	if c.ServerDescription == "" {
		c.ServerDescription = defaultDescription
	}
	if c.SearchBatchSize <= 0 {
		c.SearchBatchSize = defaultBatchSize
	}
	if c.StorageBackend == "" {
		c.StorageBackend = storageBackendJSON
	}
	c.StorageBackend = strings.ToLower(strings.TrimSpace(c.StorageBackend))
	switch c.StorageBackend {
	case storageBackendJSON:
		if c.CatalogPath == "" {
			return Config{}, fmt.Errorf("catalog_path is required")
		}
	case storageBackendMySQL, storageBackendPgSQL:
		if c.DatabaseDSN == "" {
			return Config{}, fmt.Errorf("database_dsn is required when storage_backend is %s", c.StorageBackend)
		}
	default:
		return Config{}, fmt.Errorf("unsupported storage_backend: %s", c.StorageBackend)
	}
	return c, nil
}

// LoadConfig reads a JSON configuration file from disk.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg.Normalize()
}
