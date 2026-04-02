# goed2k-server

`github.com/chenjia404/goed2k-server` 是一个用 Go 实现的 ED2K/eMule Server，面向 `github.com/monkeyWie/goed2k` 客户端协议做兼容实现。

当前版本重点提供两部分能力：

- ED2K/eMule TCP Server 协议服务
- HTTP 管理接口

项目目标不是复刻完整的 eMule 官方服务端，而是提供一个可运行、可测试、可扩展的服务端基础实现，便于你继续扩展协议能力和业务逻辑。

## 功能特性

### 已实现的 ED2K 协议能力

- 客户端登录握手 `LoginRequest`
- 服务端状态返回 `Status`
- 服务端消息 `Message`
- 客户端 ID 分配 `IdChange`
- 共享文件注册 `OP_OFFERFILES`
- 搜索请求 `SearchRequest`
- 搜索翻页 `SearchMore`
- 文件来源查询 `GetFileSources`
- 回调请求 `CallbackRequest`
- 回调通知 `CallbackRequestIncoming`
- 回调失败 `CallbackRequestFailed`

### 已实现的运行时能力

- eMule 兼容的 **UDP 服务状态**（`OP_GLOBSERVSTATREQ` / `OP_GLOBSERVSTATRES`，用于客户端刷新软性/硬性文件限制等）
- 动态用户表
- 运行时统计
- 动态共享文件注册、更新、断链撤销
- 静态共享目录持久化到 JSON、MySQL 或 PostgreSQL
- HTTP 管理接口
- 管理接口 Token 鉴权
- 列表分页、过滤、排序
- 健康检查

## 项目结构

- [cmd/goed2k-server/main.go](goed2k-server/cmd/goed2k-server/main.go): 启动入口
- [ed2ksrv/server.go](goed2k-server/ed2ksrv/server.go): TCP 服务、动态用户表、统计
- [ed2ksrv/server_udp.go](goed2k-server/ed2ksrv/server_udp.go): ED2K UDP 服务状态应答
- [ed2ksrv/admin.go](goed2k-server/ed2ksrv/admin.go): HTTP 管理接口
- [ed2ksrv/catalog.go](goed2k-server/ed2ksrv/catalog.go): 共享文件目录和持久化
- [ed2ksrv/offerfiles.go](goed2k-server/ed2ksrv/offerfiles.go): `OP_OFFERFILES` 协议处理
- [ed2ksrv/protocol.go](goed2k-server/ed2ksrv/protocol.go): 搜索请求解析
- [ed2ksrv/config.go](goed2k-server/ed2ksrv/config.go): 配置结构
- [config.example.json](goed2k-server/config.example.json): 示例配置
- [testdata/catalog.json](goed2k-server/testdata/catalog.json): 示例共享目录

## 安装与引用

### 作为命令行程序运行

如果你的仓库已经发布到 GitHub，可以直接按模块路径安装：

```bash
go install github.com/chenjia404/goed2k-server/cmd/goed2k-server@latest
```

安装后可直接运行：

```bash
goed2k-server -config config.json
```

### 作为 Go 模块引用

如果你要在自己的项目里引用服务端库包：

```bash
go get github.com/chenjia404/goed2k-server@latest
```

导入方式：

```go
import "github.com/chenjia404/goed2k-server/ed2ksrv"
```

### goed2k 依赖版本管理

当前项目直接依赖远程模块：

```text
github.com/monkeyWie/goed2k v0.0.0-20260319015208-6257e6988ff2
```

查看当前版本：

```bash
go list -m github.com/monkeyWie/goed2k
```

升级到上游最新版本：

```bash
go get github.com/monkeyWie/goed2k@latest
go mod tidy
```

固定到指定提交对应的 pseudo-version：

```bash
go get github.com/monkeyWie/goed2k@v0.0.0-20260319015208-6257e6988ff2
go mod tidy
```

升级后建议执行：

```bash
go test ./...
```

说明：

- `github.com/monkeyWie/goed2k` 当前没有稳定 tag，因此 Go 会使用 pseudo-version
- 这种版本格式本质上对应某次具体提交，适合可重复构建
- 如果后续上游发布正式 tag，可以再切到语义化版本

## 运行要求

- Go 1.25+
- 可访问 `github.com/monkeyWie/goed2k` 模块

## 快速开始

### 1. 准备配置文件

复制示例配置：

```bash
cp config.example.json config.json
```

示例配置内容：

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
  "aux_port": 0,
  "protocol_obfuscation": true,
  "server_udp": true,
  "udp_port_offset": 4,
  "soft_files_limit": 5000,
  "hard_files_limit": 200000,
  "max_users_advertised": 500000
}
```

完整字段说明见下文「配置项说明」及仓库根目录 [`config.example.json`](config.example.json)。

### 2. 启动服务

使用源码启动：

```bash
go run github.com/chenjia404/goed2k-server/cmd/goed2k-server -config config.json
```

如果你在仓库目录里，也可以：

```bash
go run ./cmd/goed2k-server -config config.json
```

启动后默认监听：

- ED2K TCP 服务: `:4661`
- HTTP 管理接口: `:8080`
- ED2K UDP（可选，见下）: TCP 监听端口 + `udp_port_offset`（默认 **+4**，即 TCP 为 `4661` 时 UDP 为 **4665**）

### UDP 端口说明（eMule / aMule 客户端）

eMule 会向服务器的 **UDP** 端口发送全局服务状态请求（`OP_GLOBSERVSTATREQ`），服务端应答 `OP_GLOBSERVSTATRES` 后，客户端才能更新服务器列表中的 **软性文件限制、硬性文件限制、最大用户数** 等字段；仅连 TCP 时这些项常为 0。

- **端口计算**：`UDP 端口 = TCP 监听端口 + udp_port_offset`。默认 `udp_port_offset` 为 **4**（与常见 eD2k 客户端约定一致，对应 aMule `SendUDPPacket` 的默认偏移）。
- **关闭 UDP**：配置 `"server_udp": false` 即可不监听 UDP（客户端上述统计仍可能显示为 0 或旧值）。
- **防火墙 / 安全组**：除放行 ED2K **TCP** 外，若启用 `server_udp`，请同步放行对应 **UDP** 端口。

## Docker 运行

线上使用的 Docker 镜像为 Docker Hub 上的 [`chenjia404/goed2k-server`](https://hub.docker.com/r/chenjia404/goed2k-server)。

拉取镜像：

```bash
docker pull chenjia404/goed2k-server:latest
```

容器默认执行 `/app/goed2k-server`，参数为 `-config /app/config.json`（与本仓库根目录 [`Dockerfile`](Dockerfile) 一致）。将主机上的 `config.json` 挂载到 `/app/config.json`，并映射端口即可运行：

```bash
docker run -d --name goed2k-server \
  -p 4661:4661 -p 4665:4665/udp -p 8080:8080 \
  -v /path/to/config.json:/app/config.json:ro \
  chenjia404/goed2k-server:latest
```

其中 `4665:4665/udp` 对应默认 TCP `4661` 且 `udp_port_offset` 为 `4` 时的 UDP 端口；若你修改了 `listen_address` 的 TCP 端口，请按 **`TCP 端口 + udp_port_offset`** 调整 UDP 映射。

当 `storage_backend` 为 `json` 时，`catalog_path` 必须指向容器内真实存在的文件，一般通过挂载静态目录或单个 catalog 文件实现，并让配置里的路径与挂载路径一致。示例：主机目录 `/srv/goed2k/`，配置中 `catalog_path` 设为 `/data/catalog.json`：

```bash
docker run -d --name goed2k-server \
  -p 4661:4661 -p 4665:4665/udp -p 8080:8080 \
  -v /srv/goed2k/config.json:/app/config.json:ro \
  -v /srv/goed2k/catalog.json:/data/catalog.json:ro \
  chenjia404/goed2k-server:latest
```

如需使用其他配置文件路径，可在镜像名之后追加参数（会覆盖默认的 `-config /app/config.json`）：

```bash
docker run --rm -p 4661:4661 -p 4665:4665/udp -p 8080:8080 \
  -v /path/to/other.json:/other/config.json:ro \
  chenjia404/goed2k-server:latest -config /other/config.json
```

也可从源码自行构建镜像，见仓库根目录的 `Dockerfile`。

## 配置项说明

| 字段 | 说明 |
| --- | --- |
| `listen_address` | ED2K 服务监听地址 |
| `admin_listen_address` | HTTP 管理接口监听地址 |
| `admin_token` | 管理接口 Token，非空时必须通过 `X-Admin-Token` 访问 |
| `server_name` | 服务名称 |
| `server_description` | 服务描述 |
| `message` | 客户端连接后收到的服务端消息 |
| `storage_backend` | 持久化后端，支持 `json`、`mysql`、`pgsql` |
| `catalog_path` | `json` 后端使用的静态共享目录文件路径 |
| `database_dsn` | `mysql` 或 `pgsql` 后端使用的连接串 |
| `database_table` | 数据库存储表名，默认 `shared_files` |
| `search_batch_size` | 每次搜索分页返回的结果条数 |
| `tcp_flags` | `IdChange` 中返回的 TCP 标志 |
| `aux_port` | `IdChange` 中返回的附加端口 |
| `protocol_obfuscation` | 是否对非 ED2K 首字节的连接做 eMule 风格 TCP 混淆（DH+RC4） |
| `server_udp` | 是否启用 UDP 服务状态应答（默认 `true`） |
| `udp_port_offset` | UDP 监听端口相对 TCP 的偏移（默认 `4`，即 TCP `4661` → UDP `4665`） |
| `soft_files_limit` | 在 UDP 应答中通告的软性文件限制（供 eMule 显示与发布策略） |
| `hard_files_limit` | 在 UDP 应答中通告的硬性文件限制 |
| `max_users_advertised` | 在 UDP 应答中通告的最大用户数 |

### 数据库存储示例

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

当使用数据库后端时：

- 启动时会自动建表
- 静态共享目录会从数据库加载到内存索引
- 管理接口对静态文件的新增、删除、持久化会写回数据库
- 运行时 `OP_OFFERFILES` 动态共享仍然只保存在内存里

## 共享目录格式

共享目录由 `catalog_path` 指向的 JSON 文件提供。

示例：

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

### 字段说明

| 字段 | 说明 |
| --- | --- |
| `hash` | ED2K 文件 Hash |
| `name` | 文件名 |
| `size` | 文件大小 |
| `file_type` | 文件类型，例如 `Iso`、`Audio` |
| `extension` | 扩展名 |
| `media_codec` | 媒体编码，可选 |
| `media_length` | 媒体时长，可选 |
| `media_bitrate` | 媒体码率，可选 |
| `sources` | 来源数，未填时默认取 `endpoints` 数量 |
| `complete_sources` | 完整来源数，未填时默认等于 `sources` |
| `endpoints` | 可返回给客户端的来源地址列表 |

## 动态共享文件注册

客户端登录后可以通过 `OP_OFFERFILES (0x15)` 向服务端注册共享文件。

当前实现策略：

- 客户端上报的共享文件进入运行时动态索引
- 动态索引参与搜索和来源查询
- 客户端断开连接后，动态共享文件自动撤销
- 动态共享文件不会写入静态 `catalog.json`

这部分是运行时会话数据，不和 HTTP 管理接口手工维护的静态目录混在同一个持久化层里。

## HTTP 管理接口

### 认证方式

当配置了 `admin_token` 时，请求头必须带：

```http
X-Admin-Token: change-me
```

### 响应格式

成功响应：

```json
{
  "ok": true,
  "data": {},
  "meta": {}
}
```

失败响应：

```json
{
  "ok": false,
  "error": "message"
}
```

### 健康检查

#### `GET /healthz`
#### `GET /api/healthz`

示例：

```bash
curl http://127.0.0.1:8080/healthz
```

### 统计信息

#### `GET /api/stats`

示例：

```bash
curl -H 'X-Admin-Token: change-me' \
  http://127.0.0.1:8080/api/stats
```

### 客户端列表

#### `GET /api/clients`

支持参数：

- `search`: 按客户端名、远端地址、监听端点、客户端 Hash 模糊过滤
- `page`: 页码，默认 `1`
- `per_page`: 每页条数，默认 `50`，最大 `500`
- `sort`: `id`、`name`、`connected_at`、`last_seen_at`

示例：

```bash
curl -H 'X-Admin-Token: change-me' \
  'http://127.0.0.1:8080/api/clients?search=test&page=1&per_page=20&sort=name'
```

### 客户端详情

#### `GET /api/clients/{id}`

示例：

```bash
curl -H 'X-Admin-Token: change-me' \
  http://127.0.0.1:8080/api/clients/2130706433
```

### 文件列表

#### `GET /api/files`

支持参数：

- `search`: 按文件名或 Hash 模糊过滤
- `file_type`: 按文件类型过滤
- `extension`: 按扩展名过滤
- `page`: 页码，默认 `1`
- `per_page`: 每页条数，默认 `50`，最大 `500`
- `sort`: `name`、`size`、`sources`

示例：

```bash
curl -H 'X-Admin-Token: change-me' \
  'http://127.0.0.1:8080/api/files?search=ubuntu&file_type=Iso&sort=size&page=1&per_page=10'
```

### 文件详情

#### `GET /api/files/{hash}`

示例：

```bash
curl -H 'X-Admin-Token: change-me' \
  http://127.0.0.1:8080/api/files/31D6CFE0D16AE931B73C59D7E0C089C0
```

### 新增或更新文件

#### `POST /api/files`

示例：

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

### 删除文件

#### `DELETE /api/files/{hash}`

示例：

```bash
curl -X DELETE \
  -H 'X-Admin-Token: change-me' \
  http://127.0.0.1:8080/api/files/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
```

### 手动持久化目录

#### `POST /api/persist`

示例：

```bash
curl -X POST \
  -H 'X-Admin-Token: change-me' \
  http://127.0.0.1:8080/api/persist
```

## 测试

运行全部测试：

```bash
go test ./...
```

当前测试覆盖：

- 搜索请求解码
- ED2K 握手
- 共享文件注册 `OP_OFFERFILES`
- 搜索与翻页
- 来源查询
- 管理接口鉴权
- 健康检查
- 客户端详情/列表
- 文件详情/列表/增删
- 目录持久化
- 统计接口

## 当前限制

当前实现仍然有明确边界：

- 参考客户端 `goed2k` 当前仓库里还没有现成的 `OP_OFFERFILES` 发送实现，服务端已支持，但客户端侧仍需补发送逻辑
- 动态共享索引目前是内存态，不做跨重启恢复
- 没有实现完整的服务端共享发布协议流中的高级特性，例如增量更新和更细粒度的发布状态同步
- 没有实现用户身份认证、权限分级、审计日志落盘
- 没有实现 Web UI
- 没有实现数据库存储，静态目录当前为 JSON 文件持久化

## 后续建议

建议下一步优先做下面几项之一：

1. 在 `goed2k` 客户端里补 `OP_OFFERFILES` 发送逻辑
2. 增加 OpenAPI 文档和 Swagger UI
3. 增加 RBAC 和审计日志
4. 把静态共享目录从 JSON 切到 SQLite/PostgreSQL
