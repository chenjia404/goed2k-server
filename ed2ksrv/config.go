package ed2ksrv

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	defaultListenAddress      = ":4661"
	defaultAdminListenAddress = ":8080"
	defaultServerName         = "goed2k-server"
	defaultDescription        = "Minimal eD2k/eMule compatible server"
	defaultBatchSize          = 200
	defaultCatalogPath        = "catalog.json"
	defaultDatabaseTable      = "shared_files"
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
	// ServerUDP listens on TCP 端口 + UDPPortOffset（默认 +4），响应 OP_GLOBSERVSTATREQ，向 eMule 通告软性/硬性文件限制等。
	ServerUDP          bool   `json:"server_udp"`
	UDPPortOffset      int    `json:"udp_port_offset"`
	SoftFilesLimit     int32  `json:"soft_files_limit"`
	HardFilesLimit     int32  `json:"hard_files_limit"`
	MaxUsersAdvertised uint32 `json:"max_users_advertised"`
}

// DefaultConfig returns a working baseline configuration.
func DefaultConfig() Config {
	return Config{
		ListenAddress:       defaultListenAddress,
		AdminListenAddress:  defaultAdminListenAddress,
		ServerName:          defaultServerName,
		ServerDescription:   defaultDescription,
		Message:             "Welcome to goed2k-server",
		StorageBackend:      storageBackendJSON,
		CatalogPath:         defaultCatalogPath,
		DatabaseTable:       defaultDatabaseTable,
		SearchBatchSize:     defaultBatchSize,
		ProtocolObfuscation: true,
		ServerUDP:           true,
		UDPPortOffset:       4,
		SoftFilesLimit:      5000,
		HardFilesLimit:      200000,
		MaxUsersAdvertised:  500000,
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
	if c.UDPPortOffset == 0 {
		c.UDPPortOffset = 4
	}
	if c.SoftFilesLimit <= 0 {
		c.SoftFilesLimit = 5000
	}
	if c.HardFilesLimit <= 0 {
		c.HardFilesLimit = 200000
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
// 若 path 指向的文件不存在，则使用 DefaultConfig 并经 Normalize（与「仅含默认值的配置文件」等价）。
// 第二个返回值为 true 表示因文件不存在而采用了内置默认配置。
func LoadConfig(path string) (Config, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg, err := DefaultConfig().Normalize()
			return cfg, true, err
		}
		return Config{}, false, err
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, false, err
	}
	cfg, err = cfg.Normalize()
	return cfg, false, err
}
