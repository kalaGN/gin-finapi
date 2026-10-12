# Gin FinAPI AI Coding 入口

## 项目定位

Gin FinAPI 是一个单仓库、单 Go module 的 Gin HTTP 查询服务。当前真实启用的接口为
`GET /health` 和 `POST /loan`；Loan 请求查询 MongoDB 中的 gzip 数据并映射为变量结果。
Redis String `gin-finapi:product-map:v1:<pcode>` 提供 JSON 变量映射；Model 和 HelloWorld gRPC 尚未进入主调用链；未使用的 LoanRepository 接口已移除。

## 技术与入口

- Go `1.24.0`，toolchain `go1.24.10`；Gin `1.11.0`。
- 应用入口：`main.go`（信号与退出码）→ `bootstrap.Run`（装配与生命周期）。
- 路由：`routes/api.go`；中间件：`app/middleware/`。
- 核心处理：LoanHandler → `app/loan/query.go` → `pkg/mongo/store.go`（实例配置与客户端）。
- `app/auth/` 定义认证凭据和错误；Handler/认证中间件不直接引用存储包。
- `/loan` 认证：`app/middleware/loan_auth.go`；MD5 phone 截前 20 位查 t，旧标识兼容。
- JSON nonce 为必填非空字符串；签名字段为 apikey、nonce、pcode、原始 phone、timestamp，排序拼接后附 secret 做 MD5；无防重放去重。
- SHA-256 phone 转换服务尚未接入（TODO），当前返回 503/9002，不查询 Mongo。
- `/loan` 凭据来自 Redis String `gin-finapi:auth:apikey:v1:<sha256(apikey)>`，每 Key 独立 secret、status、expires_at；不读取 API_KEY/SECRET_KEY，不自动写入。
- API Key 缺失/禁用/到期为 400/2001；产品授权缺失/禁用/到期为 403/2004；Redis 故障或损坏记录为 503/9002。
- `app/configdata/` 统一缓存凭据、产品授权、产品映射；内存优先、未命中读 Redis，每 5 分钟后台刷新，不检查缓存年龄。
- `REDIS_CACHE_ENABLED` 默认 true；false 时逐请求直读 Redis，不回填或启动刷新，修改后重启生效。
- 每请求检查 status/expires_at；刷新临时故障保留快照，删除/损坏清除，禁用拒绝。容量 LRU/并发参数见 `.env.example`。
- 产品映射：`pkg/redis/product_map.go`；缺失或无效为 200/6001，Redis 故障为 503/9002。
- Redis 使用既有 REDIS_* 配置，新增 `REDIS_TIMEOUT` 默认 1s；禁止自动写入或覆盖产品映射。
- 认证与产品映射各自拥有一个 Redis 池，REDIS_POOL_SIZE 是每池参数，不是应用总连接上限。
- 测试数据 CLI：`cmd/init_mongo.go`。它会删除并重建 16 个集合，未经明确授权不得运行。
- Redis 初始化：`cmd/init_redis.go`，SET NX 写入产品映射；`-migrate-map` 显式迁移旧 Hash，保留源及已有目标，授权后才可运行。
- 依赖锁定：`go.mod`、`go.sum`。CI 为最小 GitHub Actions 门禁
  （`.github/workflows/ci.yml`：`gofmt`、`go vet`、`go test -race`、`go build`，及 Redis CLI 独立检查）；
  仍未配置 Dockerfile、Kubernetes 及项目级 Lint 工具。

## 常用命令

在仓库根目录执行：

```bash
go mod download
go mod verify
go run main.go
gofmt -l $(git ls-files '*.go')
go vet ./...
go test ./...
go test -race ./...
go build ./...
go build -o /tmp/gin-finapi main.go
go test -race cmd/init_redis.go cmd/init_redis_test.go
go vet cmd/init_redis.go
```

启动依赖 `.env` 或等价环境变量；不得输出或提交真实密钥、连接串和个人数据。
关闭顺序为 HTTP → 缓存 worker/回源 → Mongo Store → Redis；HTTP/依赖各 5s 预算，HTTP 失败强制 Close。
服务退出等待另有最多 5s 保护；错误返回到入口决定失败退出码，不由 goroutine Fatal。

## 渐进加载

1. 先读 [`doc/README.md`](doc/README.md)，按任务类型选择文档。
2. 修改前读相关源文件、测试、Rules，以及对应 Spec/Knowledge；不要一次加载无关文档。
3. 当前实现与历史文档冲突时以已验证代码为事实，并报告冲突；不能静默改契约。

## 核心红线

- 不擅自改变 HTTP、Proto、业务码、Mongo 文档格式或其他公开/数据契约。
- 不提交 `.env`、Token、证书、连接串、完整手机号、API Key、签名或响应敏感数据。
- 不因依赖已存在就宣称能力已启用；以主调用链和启动代码为准。
- 不无需求增加依赖、层级、服务、中间件、基础设施或部署组件。
- 新功能和缺陷修复必须同步测试；Bug 修复遵循“复现→失败测试→根因→最小修复→回归”。
- 数据、外部依赖、架构、部署、安全或性能变更先评估影响；复杂变更先建 Spec。
- 不吞掉错误、删除测试或降低校验来让检查通过。
- 不手改 `*.pb.go`；Proto 兼容性和生成方式未确认前停止并报告。
- 禁止未经确认运行 `cmd/init_mongo.go`、`cmd/init_mongo.sh` 或等价数据重建操作。
- 不夹带无关格式化/清理，不自动创建 Git Commit。

## 修改前后

修改前：确认需求与契约、检查 `git status`、定位测试和事实来源、识别敏感/破坏性操作。

修改后：检查最小 diff，运行适用的格式、静态检查、测试和构建命令；同步 Rules、Spec、
Knowledge 或 README 中受影响的事实；明确未验证项和残余风险。
