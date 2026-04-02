# goed2k-server

[简体中文](README-CN.md)

`github.com/chenjia404/goed2k-server` is an ED2K/eMule server implemented in Go, compatible with the `github.com/monkeyWie/goed2k` client protocol.

The current release focuses on two areas:

- ED2K/eMule TCP server protocol
- HTTP administration API

The goal is not to replicate the full official eMule server, but to provide a runnable, testable, extensible server foundation so you can extend protocol behavior and business logic.

## Features

### ED2K protocol (implemented)

- Client login handshake `LoginRequest`
- Server status `Status`
- Server message `Message`
- Client ID assignment `IdChange`
- Shared file registration `OP_OFFERFILES`
- Search `SearchRequest`
- Search pagination `SearchMore`
- File source lookup `GetFileSources`
- Callback request `CallbackRequest`
- Callback notification `CallbackRequestIncoming`
- Callback failure `CallbackRequestFailed`

### Runtime (implemented)

- Dynamic user table
- Runtime statistics
- Dynamic shared file registration, updates, and revocation on disconnect
- Static shared catalog persisted to JSON, MySQL, or PostgreSQL
- HTTP admin API
- Admin API token authentication
- List pagination, filtering, and sorting
- Health checks

## Project layout

- [cmd/goed2k-server/main.go](cmd/goed2k-server/main.go): entry point
- [ed2ksrv/server.go](ed2ksrv/server.go): TCP server, dynamic user table, stats
- [ed2ksrv/admin.go](ed2ksrv/admin.go): HTTP admin API
- [ed2ksrv/catalog.go](ed2ksrv/catalog.go): shared catalog and persistence
- [ed2ksrv/offerfiles.go](ed2ksrv/offerfiles.go): `OP_OFFERFILES` handling
- [ed2ksrv/protocol.go](ed2ksrv/protocol.go): search request parsing
- [ed2ksrv/config.go](ed2ksrv/config.go): configuration structs
- [config.example.json](config.example.json): sample config
- [testdata/catalog.json](testdata/catalog.json): sample shared catalog

## Install and import

### Run as a CLI

If the module is published on GitHub, install by module path:

```bash
go install github.com/chenjia404/goed2k-server/cmd/goed2k-server@latest
```

Then run:

```bash
goed2k-server -config config.json
```

### Import as a Go module

To use the server package in your own project:

```bash
go get github.com/chenjia404/goed2k-server@latest
```

Import:

```go
import "github.com/chenjia404/goed2k-server/ed2ksrv"
```

### goed2k dependency version

This project depends on the remote module:

```text
github.com/monkeyWie/goed2k v0.0.0-20260319015208-6257e6988ff2
```

Check the resolved version:

```bash
go list -m github.com/monkeyWie/goed2k
```

Upgrade to the latest upstream:

```bash
go get github.com/monkeyWie/goed2k@latest
go mod tidy
```

Pin to a specific pseudo-version:

```bash
go get github.com/monkeyWie/goed2k@v0.0.0-20260319015208-6257e6988ff2
go mod tidy
```

After upgrading, run:

```bash
go test ./...
```

Notes:

- `github.com/monkeyWie/goed2k` does not have stable tags yet, so Go uses pseudo-versions
- A pseudo-version maps to a specific commit and is suitable for reproducible builds
- When upstream publishes semantic tags, you can switch to those

## Requirements

- Go 1.25+
- Network access to the `github.com/monkeyWie/goed2k` module

## Quick start

### 1. Create a config file

Copy the example:

```bash
cp config.example.json config.json
```

Example contents:

```json
{
  "listen_address": ":4661",
  "admin_listen_address": ":8080",
  "admin_token": "change-me",
  "server_name": "goed2k-server",
  "server_description": "Minimal eD2k/eMule compatible server",
  "message": "Welcome to goed2k-server",
  "storage_backend": "json",
  "catalog_path": "testdata/catalog.json",
  "database_dsn": "",
  "database_table": "shared_files",
  "search_batch_size": 2,
  "tcp_flags": 0,
  "aux_port": 0
}
```

### 2. Start the server

From module source:

```bash
go run github.com/chenjia404/goed2k-server/cmd/goed2k-server -config config.json
```

Or from a local clone:

```bash
go run ./cmd/goed2k-server -config config.json
```

Default listeners:

- ED2K TCP: `:4661`
- HTTP admin: `:8080`

## Docker

The image published for production is [`chenjia404/goed2k-server`](https://hub.docker.com/r/chenjia404/goed2k-server) on Docker Hub.

Pull:

```bash
docker pull chenjia404/goed2k-server:latest
```

The container entrypoint runs `/app/goed2k-server` with default arguments `-config /app/config.json` (see the [`Dockerfile`](Dockerfile) in this repo). Map host ports and mount your `config.json` at `/app/config.json`:

```bash
docker run -d --name goed2k-server \
  -p 4661:4661 -p 8080:8080 \
  -v /path/to/config.json:/app/config.json:ro \
  chenjia404/goed2k-server:latest
```

When `storage_backend` is `json`, ensure `catalog_path` refers to a file that exists inside the container—usually by mounting your catalog and pointing `catalog_path` at that path. Example: host files under `/srv/goed2k/`, with `catalog_path` set to `/data/catalog.json`:

```bash
docker run -d --name goed2k-server \
  -p 4661:4661 -p 8080:8080 \
  -v /srv/goed2k/config.json:/app/config.json:ro \
  -v /srv/goed2k/catalog.json:/data/catalog.json:ro \
  chenjia404/goed2k-server:latest
```

To use another config path, pass arguments after the image name (overriding the default `-config /app/config.json`):

```bash
docker run --rm -p 4661:4661 -p 8080:8080 \
  -v /path/to/other.json:/other/config.json:ro \
  chenjia404/goed2k-server:latest -config /other/config.json
```

To build and run from source instead of the Hub image, use the `Dockerfile` at the repository root.

## Configuration

| Field | Description |
| --- | --- |
| `listen_address` | ED2K TCP listen address |
| `admin_listen_address` | HTTP admin listen address |
| `admin_token` | Admin token; if non-empty, requests must send `X-Admin-Token` |
| `server_name` | Server name |
| `server_description` | Server description |
| `message` | Message clients receive after connecting |
| `storage_backend` | Persistence backend: `json`, `mysql`, or `pgsql` |
| `catalog_path` | Path to the static shared catalog file for the `json` backend |
| `database_dsn` | Connection string for `mysql` or `pgsql` |
| `database_table` | Table name (default `shared_files`) |
| `search_batch_size` | Results per search page |
| `tcp_flags` | TCP flags returned in `IdChange` |
| `aux_port` | Auxiliary port returned in `IdChange` |

### Database storage examples

MySQL:

```json
{
  "storage_backend": "mysql",
  "database_dsn": "user:password@tcp(127.0.0.1:3306)/goed2k?charset=utf8mb4&parseTime=true",
  "database_table": "shared_files"
}
```

PostgreSQL:

```json
{
  "storage_backend": "pgsql",
  "database_dsn": "postgres://user:password@127.0.0.1:5432/goed2k?sslmode=disable",
  "database_table": "shared_files"
}
```

When using a database backend:

- Tables are created automatically on startup
- The static catalog is loaded from the database into the in-memory index
- Admin API create/delete/persist operations write back to the database
- Runtime `OP_OFFERFILES` dynamic shares remain in memory only

## Shared catalog format

The shared catalog is a JSON file pointed to by `catalog_path`.

Example:

```json
{
  "files": [
    {
      "hash": "31D6CFE0D16AE931B73C59D7E0C089C0",
      "name": "ubuntu-24.04-desktop-amd64.iso",
      "size": 6144000000,
      "file_type": "Iso",
      "extension": "iso",
      "sources": 12,
      "complete_sources": 10,
      "endpoints": [
        {
          "host": "127.0.0.1",
          "port": 4662
        }
      ]
    }
  ]
}
```

### Field reference

| Field | Description |
| --- | --- |
| `hash` | ED2K file hash |
| `name` | File name |
| `size` | File size |
| `file_type` | File type, e.g. `Iso`, `Audio` |
| `extension` | Extension |
| `media_codec` | Media codec (optional) |
| `media_length` | Media duration (optional) |
| `media_bitrate` | Media bitrate (optional) |
| `sources` | Source count; defaults to `endpoints` length if omitted |
| `complete_sources` | Complete sources; defaults to `sources` if omitted |
| `endpoints` | List of source addresses returned to clients |

## Dynamic shared file registration

After login, clients can register shared files with `OP_OFFERFILES (0x15)`.

Current behavior:

- Reported files enter a runtime dynamic index
- The dynamic index participates in search and source queries
- On disconnect, dynamic shares are revoked automatically
- Dynamic shares are not written to the static `catalog.json`

This is session/runtime data and is not mixed with the static catalog maintained via the HTTP admin API in the same persistence layer.

## HTTP admin API

### Authentication

When `admin_token` is set, include:

```http
X-Admin-Token: change-me
```

### Response shape

Success:

```json
{
  "ok": true,
  "data": {},
  "meta": {}
}
```

Error:

```json
{
  "ok": false,
  "error": "message"
}
```

### Health

#### `GET /healthz`
#### `GET /api/healthz`

Example:

```bash
curl http://127.0.0.1:8080/healthz
```

### Statistics

#### `GET /api/stats`

Example:

```bash
curl -H 'X-Admin-Token: change-me' \
  http://127.0.0.1:8080/api/stats
```

### Client list

#### `GET /api/clients`

Query parameters:

- `search`: filter by client name, remote address, listen endpoint, or client hash (substring)
- `page`: page number (default `1`)
- `per_page`: page size (default `50`, max `500`)
- `sort`: `id`, `name`, `connected_at`, `last_seen_at`

Example:

```bash
curl -H 'X-Admin-Token: change-me' \
  'http://127.0.0.1:8080/api/clients?search=test&page=1&per_page=20&sort=name'
```

### Client detail

#### `GET /api/clients/{id}`

Example:

```bash
curl -H 'X-Admin-Token: change-me' \
  http://127.0.0.1:8080/api/clients/2130706433
```

### File list

#### `GET /api/files`

Query parameters:

- `search`: filter by file name or hash (substring)
- `file_type`: filter by file type
- `extension`: filter by extension
- `page`: page number (default `1`)
- `per_page`: page size (default `50`, max `500`)
- `sort`: `name`, `size`, `sources`

Example:

```bash
curl -H 'X-Admin-Token: change-me' \
  'http://127.0.0.1:8080/api/files?search=ubuntu&file_type=Iso&sort=size&page=1&per_page=10'
```

### File detail

#### `GET /api/files/{hash}`

Example:

```bash
curl -H 'X-Admin-Token: change-me' \
  http://127.0.0.1:8080/api/files/31D6CFE0D16AE931B73C59D7E0C089C0
```

### Create or update file

#### `POST /api/files`

Example:

```bash
curl -X POST \
  -H 'X-Admin-Token: change-me' \
  -H 'Content-Type: application/json' \
  http://127.0.0.1:8080/api/files \
  -d '{
    "hash":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
    "name":"runtime-added-demo.mp3",
    "size":4096,
    "file_type":"Audio",
    "extension":"mp3",
    "endpoints":[{"host":"127.0.0.9","port":4662}]
  }'
```

### Delete file

#### `DELETE /api/files/{hash}`

Example:

```bash
curl -X DELETE \
  -H 'X-Admin-Token: change-me' \
  http://127.0.0.1:8080/api/files/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
```

### Persist catalog manually

#### `POST /api/persist`

Example:

```bash
curl -X POST \
  -H 'X-Admin-Token: change-me' \
  http://127.0.0.1:8080/api/persist
```

## Tests

Run all tests:

```bash
go test ./...
```

Coverage includes:

- Search request decoding
- ED2K handshake
- Shared file registration `OP_OFFERFILES`
- Search and pagination
- Source queries
- Admin API authentication
- Health checks
- Client detail/list
- File detail/list/create/delete
- Catalog persistence
- Statistics API

## Current limitations

- The reference `goed2k` client does not yet ship `OP_OFFERFILES` send logic; the server supports it, but the client still needs sending implemented
- The dynamic share index is in-memory only and is not restored across restarts
- Advanced publish flows (incremental updates, finer-grained publish state) are not implemented
- No user authentication, RBAC, or audit log persistence
- No Web UI
- No database storage; the static catalog is currently persisted as JSON files only

## Suggested next steps

1. Add `OP_OFFERFILES` send logic in the `goed2k` client
2. Add OpenAPI docs and Swagger UI
3. Add RBAC and audit logging
4. Migrate static catalog from JSON to SQLite/PostgreSQL where appropriate
