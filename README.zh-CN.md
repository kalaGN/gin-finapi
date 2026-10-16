# Gin FinAPI

[English](README.md) | [简体中文](README.zh-CN.md)

基于 Gin 的 Go HTTP 查询服务。Redis 提供凭据、产品授权与变量映射，MongoDB 保存 gzip/CSV 查询数据；
可选 MySQL 持久化配置，并提供回源与同步。

## 技术与接口

- Go `1.24.0`，toolchain `go1.24.10`；Gin `1.11.0`，依赖锁定见 [go.mod](go.mod)。
- `GET /health`：存活检查，不代表数据库就绪。
- `POST /api`：API Key、签名及产品授权校验后的数据查询。
- `DELETE /config/cache`：清理本进程配置缓存，默认关闭，需独立管理 Token。

## 快速开始

需要 Go、Redis 和 MongoDB；MySQL 默认关闭。以下命令在仓库根目录执行：

```bash
go mod download
cp .env.example .env
```

已有 `.env` 时不要覆盖。编辑配置，准备受控的 API Key、产品授权、变量映射和查询数据后启动：

```bash
go run main.go
```

默认端口为 `8082`：

```bash
curl http://127.0.0.1:8082/health
```

应用不自动初始化业务数据；Mongo初始化工具会重建集合，禁止未经授权运行。
启用MySQL时默认开启向Redis同步，首次启用前须核对目标和覆盖范围。
业务请求需实时生成签名，示例与导入用例见下方文档。

## 开发检查

```bash
go mod verify
gofmt -l $(git ls-files '*.go')
go vet ./...
go test -race ./...
go build ./...
git diff --check
```

## 文档

- [接口与鉴权](doc/knowledge/api_reference.md)：请求、响应、签名、phone规则及缓存管理接口。
- [配置与存储](doc/knowledge/configuration.md)：环境变量、缓存、Redis映射与MySQL入口。
- [开发、部署与日志](doc/knowledge/operations.md)：目录职责、质量门禁、生命周期、日志格式与流程计时。
- [初始化与同步工具](cmd/README.md)、[Apifox用例](tests/APIFOX.md)、[隔离集成测试](tests/integration/README.md)。
- [完整文档索引](doc/README.md)、[AI开发入口](AGENTS.md)、[项目规则](doc/rules/README.md)。
