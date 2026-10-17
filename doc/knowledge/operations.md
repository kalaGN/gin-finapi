# 开发、部署与日志

核对日期：2026-10-16。来源：`bootstrap/`、`app/middleware/logger.go`、`pkg/logger/`、
`pkg/utils/timing.go`及CI。详细开发约束见 [Rules](../rules/project_rules.md)。

[文档索引](../README.md) · [接口与鉴权](api_reference.md) · [配置与存储](configuration.md)

## 项目结构

```text
gin-finapi/
├── app/
│   ├── Http/controllers/loan/ # LoanHandler：已认证请求、查询与 HTTP 响应
│   ├── services/             # 产品查询、phone 转换、凭据/授权快照与业务错误
│   ├── configdata/           # 内存优先读取、LRU、Redis→MySQL回源与同步编排
│   └── middleware/           # 认证、CORS、日志与可关闭的 IP 限流
├── bootstrap/                # 依赖装配、HTTP 生命周期和资源关闭
├── pkg/
│   ├── config/               # 环境配置加载与校验
│   ├── logger/               # 应用日志、异步请求日志与 JSON 格式
│   ├── mongo/                # Store、客户端生命周期和 gzip/CSV 解码
│   ├── mysql/                # 配置四表、加密、读取、快照与合成样例
│   ├── redis/                # 凭据、授权、产品映射读取与客户端配置
│   ├── request/              # HTTP DTO、严格类型解析与响应出口
│   ├── rescode/              # 对外业务码和文案
│   └── utils/                # 签名、时间戳、SID 与公共计时
├── routes/                   # GET /health、POST /api 路由定义
├── cmd/                      # 显式授权后使用的数据初始化工具
├── tests/                    # Apifox 用例、隔离集成测试与历史压测脚本
├── doc/                      # Rules、Specs、Knowledge
├── .github/workflows/ci.yml  # 格式、静态检查、竞态测试与构建门禁
├── .env.example              # 配置模板；本地 .env 不提交
└── main.go                   # 信号处理、调用 bootstrap.Run 与退出码
```

## 运行与构建

仓库根目录执行 `go run main.go`，先按 [配置说明](configuration.md) 准备环境、受控配置和查询数据。
本地热重载配置为 `.air.toml`，Air未加入应用依赖。

```bash
# Linux amd64；其他服务器须按目标CPU调整GOARCH
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/gin-finapi main.go

# 当前操作系统与CPU
go build -o /tmp/gin-finapi main.go
```

## 开发

限流配置放在 `.env` 或环境变量中，修改后重启生效。非法次数或窗口会拒绝启动。
限流仍为单进程、按 IP，作用于所有路由；多实例间不共享计数。
每 IP 从本窗口首次放行开始计时，到达窗口长度即重置；窗口不会被后续请求推迟。
这是固定窗口而非滑动窗口，边界附近可能出现相邻两个窗口的突发。
限流器构造不启动后台任务；bootstrap 显式启动清理 worker，HTTP 排空后纳入依赖预算关闭。
直接调用 `SetupRoute` 时必须执行返回的关闭函数，避免遗留清理任务。

### 添加新接口

1. 复用已有 `app/` 目录；未经明确确认，不新增目录。HTTP 处理放 Handler，业务规则放现有 `app/services/`。
2. 在 `bootstrap/route.go` 装配依赖，由 `routes/api.go` 绑定 Handler；main 保持信号与退出职责。
3. 在业务路由注册认证中间件；`/api` 必须挂载 `LoanAuthWithProduct`，新功能同步补测试。

### 添加中间件

在 `bootstrap/route.go` 中注册全局中间件：

```go
router.Use(middleware.YourMiddleware())
```

`/api` 的认证与产品授权由 `bootstrap/route.go` 注入
`middleware.LoanAuthWithProduct`；不要在路由层读取静态密钥或绕过产品授权。

### 验证与 CI

在仓库根目录执行，命令与现有 CI 工具链一致：

```bash
go mod verify
gofmt -l $(git ls-files '*.go')
go vet ./...
go test -race ./...
go build ./...
go vet cmd/init_redis.go
go test -race cmd/init_redis.go cmd/init_redis_test.go
go vet cmd/init_mysql.go
go test -race cmd/init_mysql.go cmd/init_mysql_test.go
go vet cmd/sync_mysql_redis.go
go test -race cmd/sync_mysql_redis.go cmd/sync_mysql_redis_test.go
git diff --check
```

默认测试不包含真实 Redis/Mongo 集成；隔离容器回归见 [集成测试说明](../../tests/integration/README.md)，
Apifox 导入见 [用例说明](../../tests/APIFOX.md)。数据测试只能指向专用、可丢弃环境。

## 部署边界

现有 GitHub Actions 只做检查和构建，不执行服务器部署；仓库没有 Dockerfile 或 Kubernetes 配置。
按目标 CPU 架构编译二进制，分别配置运行环境、Redis/Mongo 数据和进程管理；不要将密钥写入镜像或命令历史。
`/health` 仅返回存活响应，不代表数据库就绪；Tag、构建成功或本地测试通过也不代表已经部署。

## 生命周期

`main` 只处理信号和退出码，`bootstrap.Run` 装配并管理运行；启动或服务失败也进入依赖清理。
正常关闭为 HTTP → 限流 → MySQL同步 → 配置缓存/回源 → MySQL → Mongo Store → Redis → 请求日志，
HTTP/依赖各有独立 5s 预算；HTTP Shutdown 失败时强制 Close。
服务 goroutine 退出等待另设 5s 保护，整个过程不承诺严格 10s 硬上限；需匹配部署终止宽限时间。
Mongo 仅通过 Store 实例查询和关闭，不读取全局配置；已移除旧 GetMongo/GetClient/包级 Close 入口。

Mongo初始化在锁外合并，等待支持独立取消；MONGODB_TIMEOUT涵盖初始化等待和Find。
关闭取消初始化，迟到客户端不会发布并独立清理；关闭后不可重新打开。详见
[Mongo初始化边界](../specs/20261016-Mongo初始化边界-设计.md)。

## 日志

请求完成日志与 Handler 诊断日志统一通过 `sid` 关联；已删除 `request_id` 字段及生成逻辑。
`/api` 在认证前生成一次 SID，完成/诊断日志增加 `sid`，认证或参数失败也能关联。
成功响应复用该 SID，失败响应仍保留空 `sid`；生成失败先返回 `500/9001`，不认证、不查询。
SID 生成在全局限流之后；health、404、预检、限流拒绝及生成失败的日志 `sid` 为空，没有逐请求关联标识。
正常未查得不输出异常日志，产品映射异常使用 WARN，其余故障通过固定 `error_kind` 分类；不记录原始驱动错误。

项目使用 Logrus 进行结构化日志记录：

请求完成日志增加整数 `success` 和 `charge`，按响应业务 `status` 计算：
`0 → success=1, charge=1`；`3001 → success=1, charge=0`；其他或无业务状态均为 `0/0`。
这不是 HTTP 状态码判断，也不执行实际扣费。`status` 为业务状态码，无业务码时为 null；
`http_status` 为 HTTP 状态。`input` 记录完整请求体（含未知字段，不含 Header/Query），
`response_data` 记录实际完整响应；合法 JSON 嵌入为 JSON 值，其他内容为字符串。
删除日志顶层 `msg`，响应内的 `msg` 保留；`time` 为日志生成时的 Unix 毫秒整数。
请求已有 64 KiB 上限；超限请求仅记录前 64 KiB 并标记 `input_truncated=true`，不放宽接口校验。
按用户确认，请求体中的 API Key、签名及 phone 不脱敏；应严格限制日志访问并确定保留/清理周期。

业务 Panic 在日志中间件内恢复后仍记录完成日志。尚未写响应时保持 HTTP 500 和空响应体，
日志 `status=null`、`success=0`、`charge=0`；已写响应按实际状态和内容记录，不伪造额外错误 JSON。

请求完成日志由后台 worker 异步输出，可在 `.env` 中选择：

```env
REQUEST_LOG_OUTPUT=file
REQUEST_LOG_FILE=request.log
REQUEST_LOG_QUEUE_SIZE=10000
REQUEST_LOG_MAX_BYTES=67108864
```

改为 `REQUEST_LOG_OUTPUT=redis` 时，需另设 `CHARGE_REDIS_ADDR`，并按需要配置
`CHARGE_REDIS_PASSWORD、CHARGE_REDIS_DB、CHARGE_REDIS_POOL_SIZE、CHARGE_REDIS_TIMEOUT`。
该连接与认证/产品映射 Redis 独立，不自动复用或回退。每条请求日志为 JSON，
使用 `RPUSH request_log` 追加到 List；后续消费者可从左侧消费，本项目暂不实现消费任务。
Redis 首次失败后重试两次，共最多三次尝试；仍失败则丢弃并写脱敏内部错误日志。
队列条数或字节预算（默认 64 MiB，包含正在写入记录）达到上限时不阻塞客户响应，
丢弃并由后台汇总错误；启动/内部错误日志仍保留原输出方式，不输出请求载荷或原始敏感错误。
关闭在现有预算内尽力排空。内存队列允许丢失，网络超时重试可能重复，不能作为唯一可靠计费账本。
不自动裁剪 List 或设置 TTL；需管理消费速度、Redis 容量和文件轮转。修改配置后重启生效。

- **应用内部日志**：开发环境文本/Debug；其他环境 JSON/Info。
- **请求完成日志**：文件或 Redis 后端统一使用 JSON，不随开发环境切换为文本。

示例日志：

```json
{
  "level": "info",
  "method": "GET",
  "path": "/health",
  "http_status": 200,
  "duration_ms": {"entry": 0, "finish": 0},
  "success": 0,
  "charge": 0,
  "status": null,
  "input": "",
  "response_data": "ok",
  "time": 1791896584522
}
```

`duration_ms` 为 JSON 对象，值为距请求日志中间件入口的累计整数毫秒，不是各阶段独立耗时。
`/api` 示例：`{"entry":0,"auth":3,"product_map":4,"query":12,"finish":13}`。
auth 包含参数、凭据、验签及产品授权，成功时在业务查询前结束；失败也记录已结束的阶段，
未执行的后续节点不补0。finish 不含日志格式化、排队和异步后端写入时间，JSON字段顺序不保证。
原先将 duration_ms 当作数字读取的外部消费者需同步适配；后台缓存刷新和MySQL同步日志仍为整数。
新增统计节点可用 `pkg/utils.StartTiming(ctx)`、`MarkTiming(ctx, "固定节点名")`、`TimingSnapshot(ctx)`；
计时器经Context传递、并发安全、同名首次有效，输出独立快照，不使用请求参数作为节点名。

### Mongo内部耗时

Mongo查询额外记录以下累计节点；相邻节点相减才是对应步骤的近似耗时（毫秒取整）：

| 节点 | 表示执行到的位置 |
| --- | --- |
| mongo_start | 开始获取Mongo客户端 |
| mongo_client | 获取客户端返回；冷连接可能等待初始化，热连接复用实例 |
| mongo_find | FindOne及BSON解码返回，包含连接池等待及网络往返 |
| gzip | gzip解压返回 |
| csv_map | CSV切分、整数转换及变量映射返回 |

例如以下为说明用数据，不是实测：

```json
{"duration_ms":{"entry":0,"auth":0,"product_map":0,"mongo_start":0,"mongo_client":2,"mongo_find":28,"gzip":29,"csv_map":30,"query":30,"finish":30}}
```

客户端获取约2ms、查询及BSON解码约26ms、解压约1ms、解析映射约1ms。
不能从mongo_find直接推断数据库服务器执行时间；失败请求只记录已执行步骤（含错误返回），
未查得没有gzip/csv_map，坏gzip没有csv_map。仍不记录内部数据或敏感错误。
细分设计见 [流程计时Spec](../specs/20261016-请求流程计时-设计.md#2026-10-17-mongo内部节点扩展)。

## 历史版本 v1.0.1（2026-10-13）

本次按维护者要求采用补丁版本号；包含客户端不兼容调整，并非完全向后兼容的补丁。

- 业务接口统一为 `POST /api`，不再注册 `POST /loan`。
- phone 仅接受 32/64 字节；其他非空长度返回 HTTP 400/1002，旧短标识客户端须调整。
- 纳入 `.air.toml` 本地热重载配置，未将 Air 加入应用依赖。
- 当前签名仅包含 apikey、nonce、timestamp，附对应凭据 secret 后计算 MD5；旧签名客户端须同步。
- 飞书报警继续暂缓；SHA-256 转换服务仍未接入，64 位十六进制 phone 返回 503/9002。

应用版本由 Git Tag 管理，Go 与依赖版本仍由 `go.mod`、`go.sum` 管理；Tag 不代表已部署。

## 许可证

MIT License（当前 README 声明；仓库未提供独立 LICENSE 文件）。
