# Gin FinAPI 项目规则

本文件是长期强制约束。事实于 2026-09-12 基于 `main` 分支提交 `69cf7e5` 及
[`核心稳定性修复`](../specs/20260912-核心稳定性修复-需求.md)工作树复核；具体实现变化后必须重新
核对源代码。规则冲突处理见 [`../README.md`](../README.md)。

## 1. 项目与技术基线

- 项目是单仓库、单 Go module、单进程 Gin HTTP 服务，不是已确认的微服务集群。
- Go 版本以 `go.mod` 为准：`go 1.24.0`、`toolchain go1.24.10`。
- 直接依赖包括 Gin 1.11.0、MongoDB Driver 1.17.2、go-redis 9.7.0、go-sql-driver/mysql 1.10.1、Logrus 1.8.1 和
  godotenv 1.5.1；Protobuf 由 Gin 间接依赖。
- `go.mod`、`go.sum` 是依赖事实来源；没有其他包管理器或锁文件。
- CI 为最小 GitHub Actions 门禁：`.github/workflows/ci.yml` 执行 `gofmt` 检查、`go vet ./...`、
  `go test -race ./...`、`go build ./...`，在 `main`/`dev` 推送和 Pull Request 时触发。
- 未发现项目级 golangci-lint、Makefile、Dockerfile、Kubernetes、Nginx 或数据库迁移工具。
- 当前功能边界是健康检查、按 pcode 从 Redis 读取映射并查询 MongoDB、解压后映射变量。
  可选 MySQL 持久化客户/Key/产品/授权并回源、同步；消息队列和 GraphQL 未启用。

### 目录职责

| 路径 | 当前职责 |
| --- | --- |
| `main.go` | 信号 Context、Run、脱敏失败摘要与退出码 |
| `bootstrap/` | 配置、日志、依赖和路由装配；HTTP 生命周期和资源关闭 |
| `routes/` | `GET /health`、`POST /api` 注册 |
| `app/Http/controllers/loan/` | 已认证请求处理、复用 Context SID、调用查询标识规则和 HTTP 响应 |
| `app/services/` | 查询编排、phone 转换、凭据/授权快照与稳定错误，不依赖 Gin 或存储实现 |
| `app/middleware/` | CORS、日志、进程内限流、LoanAuthWithProduct |
| `app/configdata/` | 三类配置的内存优先读取、LRU、合并回源、显式失败回源与同步编排 |
| `pkg/config/` | 主调用链的环境配置入口 |
| `pkg/mongo/` | Store 实例、客户端、查询、gzip 解压和变量映射；无全局客户端入口 |
| `pkg/mysql/` | 配置四表、secret加密、参数化SQL、只读快照与合成样例 |
| `pkg/redis/` | 产品映射与凭据读取器，统一选项、独立池 |
| `pkg/rescode/`、`pkg/utils/` | 业务码、SID、时间戳和签名工具 |
| `cmd/` | 会重建测试集合的 Mongo CLI、用 SET NX 保留已有产品映射的 Redis CLI及显式迁移 |
| `tests/bench/` | 隔离 Go 合成基准，以及不符合当前契约的历史外部 ab 脚本 |

### 非目标

- 不因“最佳实践”主动改成微服务、Clean Architecture、DDD 或新增 Service/Repository 层。
- 不把依赖库能力或部署设想描述成现有能力。
- 不在无需求时升级 Go、框架、驱动或引入新的基础设施。
- 不在文档任务中修复业务代码、清理遗留模块或改变 API 行为。

## 2. 真实命令与质量门禁

以下命令从仓库根目录执行：

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
go build -o /tmp/gin-finapi-init-redis cmd/init_redis.go
go test -race cmd/init_mysql.go cmd/init_mysql_test.go
go test -race cmd/sync_mysql_redis.go cmd/sync_mysql_redis_test.go
go vet cmd/init_mysql.go
go vet cmd/sync_mysql_redis.go
go build -o /tmp/gin-finapi-init-mysql cmd/init_mysql.go
go build -o /tmp/gin-finapi-sync-mysql-redis cmd/sync_mysql_redis.go
```

- `go run main.go` 需要 Redis 配置、`MONGODB_DSN`、`MONGODB_DATABASE`；真实值只通过未跟踪
  的 `.env` 或运行环境注入。
- 格式门禁要求 `gofmt -l ...` 无输出。只格式化本次实际修改的 Go 文件，避免无关 diff。
- 当前没有已确认覆盖率门槛或 Lint 工具；不得虚构门槛，也不得把未配置工具设为必跑命令。
- `go mod verify` 校验下载依赖内容；依赖变更后还需检查 `go.mod`、`go.sum` 的最小 diff。
- `go build ./...` 用于编译验证，不提交生成的本地二进制。
- `go build -o /tmp/gin-finapi main.go` 验证应用可执行文件打包；仓库没有已确认的发布流水线或目标平台。
- README 记录了 Linux amd64 交叉编译示例，但真实生产平台待确认，不能把该目标当作发布门禁。

## 3. 当前架构规则

### 真实调用链

```text
main.go
  → bootstrap.Run → bootstrap.NewRouter → bootstrap.SetupRoute
  → 外层 Recovery → CORS → Logger → 业务 Recovery → 进程内 IP 限流
  → GET /health
  → POST /api
      → RequestSID 生成并保存 SID（失败先返回 500/9001）
      → LoanAuthWithProduct 单次 JSON/字段/API Key/时间戳/签名及产品授权校验
      → LoanHandler 复用 SID → services.PhoneLookupKey → app/services 产品查询编排
      → app/configdata 内存优先，未命中 GET 产品 KV；MySQL启用后缺失/损坏/故障回源，校验字段映射
      → mongo.Store.Query（实例配置）
      → Mongo FindOne(t=phone)
      → gzip 解压 v → CSV 整数映射
      → CommonRes JSON
```

LoanHandler 显式注入查询和 Logger；bootstrap 装配查询用例、存储错误适配、SID 中间件、认证和限流。
HTTP DTO 放在 pkg/request；Handler/LoanAuth 不直接引用 Mongo/Redis 类型，查询与认证稳定语义统一来自 app/services。
认证 ErrAuthUnavailable 与查询 ErrUnavailable 必须区分，不因合并目录而合并错误分类或数据契约。
ProductKey、Digest、ErrInvalidRecord 及状态/到期 Validate 策略归 services；Redis 不依赖 configdata。
Redis/MySQL 负责适配，共享映射校验归 services；configdata 负责缓存/刷新/回源/同步编排，逐请求使用同一 Validate。
未使用的 LoanRepository 接口已移除，不为本查询创建 CRUD 分层。

### 职责与依赖

- 路由只负责公开路径、方法和 Handler 绑定；全局中间件装配放在 `bootstrap/`。
- HTTP 解析和响应转换保持在 Handler/Controller 边界；可复用纯逻辑放在已有 `pkg/` 能力中。
- Mongo 访问和 BSON/压缩格式处理放在数据访问边界，不向新 Handler 复制连接或解压逻辑。
- Mongo 保持同包分工：`store.go` 是实例配置/查询入口，`mongo.go` 定义 BSON 类型与超时解析，`client.go` 负责实例客户端生命周期，
  `codec.go` 负责 gzip/CSV 解码与变量映射，`errors.go` 负责错误分类；解码函数不得承担数据库 I/O。
- 配置从单一已加载的 `pkg/config` 对象读取，不得在请求路径重复加载环境变量。
- 新抽象必须解决已出现的复用、隔离外部依赖或可测试性问题；单次调用不创建空壳层。
- phone 标识转换规则归 app/services/phone_lookup.go，Handler 保留调用顺序与 HTTP 错误转换；不复制业务转换规则到 HTTP 层。
- 未经用户事先明确确认，不得在 app/ 下新增目录；复用现有职责目录，不随意增加功能包。
- 禁止循环依赖。通用包不得反向导入 Controller、Route 或 Bootstrap。

### 未建立的边界

- 查询没有应用级事务或分布式一致性协议；MySQL样例写入用事务，快照用只读可重复读事务。
  Redis同步用有界租约及Lua执行隔离，不承诺运行时错误回滚或跨MySQL/Redis/内存原子一致。
- 当前没有依赖注入容器；Mongo 配置和客户端仅由 Store 持有，不再提供全局兼容入口；查询用例与 Handler 通过函数注入。
- 新事务、异步处理或外部服务调用必须先通过 Spec 明确一致性、超时、重试和回滚。
- 现有结构不一致应记录风险，不借局部需求擅自进行全仓重构。

### 错误边界

- 对外业务码、默认文案及参数场景文案统一定义在 `pkg/rescode`，HTTP 层引用，不散落字面量。
  同一业务码的不同场景文案通过命名常量保留；内部错误按业务模块定义并用 `errors.Is` 判断，
  不直接向客户端输出 `err.Error()`，不把内部错误混入响应文案映射。
- 底层错误在数据访问边界保留原因，在 HTTP 边界转换为稳定、无敏感信息的契约错误。
- 当前 Controller 区分 Mongo 未查得、超时、不可用和数据损坏，并在 HTTP 边界映射稳定业务码；
  存储错误在装配适配边界转换成稳定语义；日志不输出可能含凭据的原始错误。
- 修改状态码、业务码或错误体可能影响客户契约，必须先确认 Spec 和兼容方案。
- 不忽略、覆盖或只打印错误；返回、分类或使用项目 logger 记录必要上下文。

## 4. HTTP 与 Proto 契约

### 已启用 HTTP

| 方法与路径 | 请求 | 当前响应 |
| --- | --- | --- |
| `GET /health` | 无业务参数 | HTTP 200，纯文本 `ok` |
| `POST /api` | `application/json`；`phone`、`pcode`、`apikey`、`timestamp`、`sign` | JSON：`status`、`msg`、`sid`、`data` |
| `DELETE /config/cache`（配置Token才启用） | 独立Authorization Bearer，无Body参数 | 成功200含cleared三类数量；认证401、关闭/取消503含error；默认404 |

- `/api` 的 `pcode` 只接受 JSON 整数且范围为 10001–99999；`timestamp` 只接受 JSON 整数，
  使用毫秒并限制为过去 5 分钟内。字符串、小数、指数形式和整数溢出值均拒绝。
- phone 是 32/64 字节的非空字符串，不参与签名；其他非空长度为 400/1002。
  所有 32 位字符串统一截前 20 位查 t，不要求十六进制，保留大小写。
  SHA-256（64 位十六进制）需后续服务映射为原手机号 MD5，目前 TODO，返回 503/9002，不直接查询。
  64 位非十六进制标识原样查询，不解密或对摘要重新哈希；旧 20 位请求标识拒绝。
- /api 认证中间件通过 JSON apikey 的 SHA-256 摘要读取 Redis String 凭据并验证启停和到期，Body 上限 64 KiB，只解析一次。
- 签名验证后，经统一读取层按 API Key+pcode 检查产品授权；未命中读 Redis，每 5 分钟后台刷新，不检查缓存年龄；逐次检查业务到期。
- 签名当前把 `apikey`、`nonce`、`timestamp` 按 key 排序拼接，加对应凭据 `secret` 后计算
  大写 MD5。该算法存在安全/演进风险，但在确认客户端兼容前不得擅自替换。
  pcode、phone 不参与验签，仍必须校验参数及产品授权；签名不保护业务字段完整性。
- nonce 必须为非空 JSON 字符串，原值签名、保留前导零；缺失/空串为 400/1001，类型错误为 400/1000。
  不接受旧四字段或五字段签名；URL Query 不替代 Body。不因参与签名就宣称已实现 nonce 防重放去重。
- 成功数据字段由产品 String KV 的映射决定；既有六变量示例仍可配置。变更映射须核对 CSV 列序和客户端兼容。
- 产品映射最终缺失或无效为 200/6001；最终配置源不可用为 503/9002，不得误报未查得。
  MYSQL_ENABLED=true 时 Redis缺失/损坏/故障回源；有效禁用/到期命中不回退，调用方取消不启动下一层。
- 当前没有分页、版本前缀、废弃策略或幂等键约定；标记“待确认”，不能自行创建。
- CORS 当前允许任意 Origin；是否收紧取决于真实调用方和部署环境，需先确认。

### 契约变更

- 新增、删除、重命名字段，改变类型/必填性、状态码、业务码、签名或时间窗口，都视为契约变更。
- 契约变更必须有需求与设计 Spec、兼容影响、客户端迁移和回滚方案，并增加契约/Handler 测试。
- HTTP 业务错误码定义来自 `pkg/rescode/rescode.go`；现状并非所有返回都一致，不能凭空统一。

### gRPC / Proto

- 当前仓库没有 Proto IDL、生成代码或 gRPC Server 启动链路。
- 如需引入 gRPC，必须先通过 Spec 明确契约、生成工具链、兼容策略、Deadline、Metadata、错误码和发布方式。
- `*.pb.go` 只能从已评审的源 IDL 和固定工具链生成，不手工修改。

## 5. Go 编码规范

- 使用 Go 1.24 支持的标准写法，提交前以 `gofmt` 和 `go vet` 为最低门禁。
- 包名短小、小写且与职责一致；导出标识符使用 Go 命名惯例并提供有价值的 GoDoc。
- 优先具体类型。对外部 JSON 使用明确 DTO；使用 `interface{}` 时必须说明动态类型边界和校验方式。
- 函数保持单一职责和清晰控制流；嵌套、分支或副作用过多时优先提取可测试纯逻辑。
- 错误使用 `%w` 保留链路（需要兼容判断时），不使用 `%v` 破坏可判定性；不吞掉错误。
- 注释解释原因、契约或风险，不复述代码；删除废弃代码，不用注释长期保留旧实现。
- 不重复已有配置、签名、SID、业务码或数据访问能力；复用前先验证其正确性与并发安全。
- 不做全仓无关格式化，不在功能变更中夹带大范围重构。

### 配置

- 新代码优先使用已加载的 `pkg/config.Config`，不得在请求路径重复 `godotenv.Load()`。
- 环境变量新增/更名是运维契约变更，需同步 `.env.example`、校验和使用文档。
- 配置解析失败不能静默回退到可能危险的默认值；必填项在启动阶段验证。
- 不记录配置值中的密钥、密码或连接串。

### 并发与资源

- Gin Handler 会并发执行；共享 map、随机源、客户端状态必须证明并发安全并有并发测试。
- SID 使用 `crypto/rand` 和拒绝采样；随机源失败必须返回错误，禁止降级到可预测值。
- `/api` 的 RequestSID 在认证/参数解析之前、全局限流之后执行；一次生成并供 Handler/日志复用。
  生成失败先返回 500/9001；认证成功却缺少 Context SID 视为装配错误，不允许 Handler 补生成。
  保持成功响应返回 SID、失败响应 sid 为空；不从客户端 Header/Body 读取服务端 SID。
- Goroutine 必须有清晰所有者、停止方式和生命周期；禁止不可停止的新增后台循环。
- 外部 I/O 传递请求 Context，并设置明确超时；不得用 `context.Background()` 丢失取消传播。
- 打开的响应体、文件、压缩流、连接和 ticker 必须在所有路径释放。
- 保留既有请求Body/日志队列边界；不擅自增加查询数据大小或字段数业务上限。大数据风险先测量，变更限制须确认。

### 依赖与生成代码

- 引入依赖前确认标准库和现有依赖不能满足，并记录用途、维护状态、安全、许可证和体积影响。
- 升级 Go、Gin、Mongo/Redis Driver 或引入 gRPC/Proto 需 Spec、兼容评估和全量回归。
- 只因依赖存在不能使用或宣称相关能力已经启用。
- 生成代码由源 IDL 和固定工具链产生；未知生成命令时停止并标记待确认。

## 6. 测试规范

### 当前事实

- 使用 Go 标准 `testing`；测试覆盖 `pkg/utils`、`pkg/rescode`、Mongo、Loan Controller、路由、
  中间件和主程序关闭编排。
- Redis 产品映射已有离线解析、读取分类和 HTTP 编排测试；真实 Redis/Mongo 回归见 [`集成测试说明`](../../tests/integration/README.md)，使用独立临时容器。
- 没有真实覆盖率门槛或 E2E CI 门禁；集成测试通过显式脚本运行，不进入默认离线测试。

### 测试层级

- Unit：纯校验、签名、映射、错误分类、边界和并发安全，不访问真实网络。
- Component：使用 `httptest` 验证 Gin 路由、中间件、请求/响应和错误转换。
- Integration：验证 Mongo 文档查询、索引、gzip 数据和故障行为；必须使用隔离数据库/集合。
- Contract：固定公开 HTTP 或 Proto 的字段、类型、状态码和兼容行为。
- End-to-End：只有具备隔离环境、测试凭据和清理方案后执行；当前尚未建立。

### 最低要求

- 新功能覆盖正常、异常和关键边界；涉及认证时覆盖缺失、无效、过期和权限场景。
- Bug 修复先提交能复现问题的失败测试，再做最小修复并运行相关及全量测试。
- 并发代码运行相关并发测试和 `go test -race ./...`；超时/取消路径必须可确定验证。
- 时间、随机和网络依赖应可注入或受控，避免 sleep、真实时钟边界和随机碰撞导致不稳定测试。
- 数据库测试不得读取、覆盖或清理生产数据；Fixture 小而明确，每个测试可独立重复运行。
- 测试名使用 `Test<对象>_<场景>` 或清晰表驱动 case，断言外部行为而非内部实现。

### 提交前门禁

```bash
gofmt -l $(git ls-files '*.go')
go vet ./...
go test ./...
go test -race ./...
go build ./...
```

若因环境、外部依赖或权限无法完成，必须报告未执行命令、原因和风险，不能写“通过”。

## 7. 安全规范

### 已确认机制

- `.env` 和 `*.ini` 被 `.gitignore` 忽略；`.env.example` 只包含占位值。
- 缓存清理只允许独立CONFIG_CACHE_TOKEN认证，不能复用客户Key；空Token不注册路由，Token不进Body/Query或日志。
  管理接口不采集Body，响应无业务status，完成日志不计费；API查询的完整Body/响应日志规则保持不变。
- 启动时校验 Redis 配置、Mongo DSN 和数据库名非空，不再依赖 PHONE_AES_KEY。
- `/api` 校验五分钟时间戳、API Key、apikey/nonce/timestamp 的MD5签名；phone按services规则转换后查询。

### 已确认缺口

- LoanAuth 经配置读取层优先读内存、Redis；启用MySQL时支持客户下多Key与独立授权，验签后检查产品授权。
- 凭据缺失/禁用/到期返回400/2001，最终源故障/损坏凭据返回503/9002；禁止环境变量凭据兜底。
  MySQL secret须独立密钥加密，禁止原始DSN/驱动错误入日志；运行账号仅需SELECT，初始化授权单独管理。
  不自动建表或写样例；仅用户确认的MySQL同步允许写三类Redis配置，不扩展到日志或其他业务数据。
- 用户已明确批准请求完成日志记录完整 Body（input）及实际响应（response_data），包括请求凭据；
  不含 Header/Query。status 为业务码，无业务码为 null；time 为 Unix 毫秒整数，删除顶层 msg。
  请求超出既有 64 KiB 限制时只记录前 64 KiB 并标记 input_truncated，接口仍拒绝超限请求；传输仍需 HTTPS。
- 请求完成日志 success/charge 为整数：业务 status=0 时 1/1，3001 时 1/0，其他或无状态时 0/0。
  业务 JSON 响应使用 pkg/request.JSON 或 AbortWithStatusJSON 保存安全状态元信息，不按 HTTP 状态推断。
  charge 只是日志标记，不代表已接入实际扣费系统。
- 请求完成日志通过后台有界队列异步写入 file 或独立 charge Redis；REQUEST_LOG_OUTPUT 选择后端。
  REQUEST_LOG_MAX_BYTES 默认 67108864 字节，包含排队及正在写入的记录，与条数上限同时生效。
  Redis 固定 RPUSH request_log，首次失败后最多重试两次，驱动不得叠加自动重试。
  队列满或最终写入失败允许丢弃，必须写安全内部错误，不阻塞响应、不递归进入请求日志队列。
  不自动 LTRIM/EXPIRE 或消费；消费者须考虑丢失/重复。日志关闭须纳入现有生命周期预算。
- 请求日志duration_ms必须为累计整数毫秒对象：entry=0、auth、product_map、query、finish，
  仅记录实际执行节点；auth在Next前结束，失败返回也记录。finish不含日志整理/排队/写入。
  公共方法为pkg/utils.StartTiming、MarkTiming、TimingSnapshot；同名首次有效、快照隔离、固定节点名，
  禁止请求输入成为节点名。后台缓存刷新/MySQL同步的duration_ms仍为整数，外部请求日志消费者须适配新类型。
  Mongo查询细分mongo_start（获取客户端前）、mongo_client（获取返回）、mongo_find（Find/BSON解码返回）、
  gzip（解压返回）、csv_map（CSV切分/整数解析/映射返回）。失败返回也标记已执行步骤，未执行的不补0；
  客户端耗时按请求等待统计，不在共享初始化goroutine中绑定首个请求；mongo_find不是纯数据库执行时间。
- 签名仍使用 MD5，但比较已使用常量时间实现；五分钟内仍没有 nonce/幂等防重放机制。
- CORS 为 `*`；允许来源、凭据需求和反向代理信任边界待确认。
- LoanHandler 不记录 Mongo/解压原始错误，客户端只返回稳定错误码和文案；
  其他边界是否泄露内部信息仍需逐项审计。

### 强制规则

- 密钥、Token、证书、密码、连接串只通过受控环境注入；示例使用明显占位值。
- 输入在 HTTP/Proto/数据边界校验类型、长度、范围和格式；拒绝隐式截断或宽松反序列化。
- 认证与授权必须在数据访问前完成；认证主体和密钥的权威来源需由 Spec 明确。
- 不拼接查询、命令、路径或模板中的不可信输入；Mongo 查询字段也需白名单化和限制大小。
- 完整 Body/响应仅允许进入用户确认的请求日志后端（见完整请求日志 Spec）；须限制权限和保留周期。
  其他日志仍禁止记录凭据或完整敏感数据；禁止真实请求样本入库。错误响应不暴露内部拓扑和原始错误。
- 新文件上传、外部 URL、重定向、模板或命令执行功能必须专项评估 SSRF、路径和注入风险。
- 依赖变更执行仓库可用的校验，并记录尚未配置漏洞扫描这一缺口。
- 高风险数据初始化必须明确目标环境、备份/回滚和人工授权；禁止对未知环境运行。
- 敏感数据保留、加密、最小权限和删除周期目前待确认，涉及变更前先建 Spec。

## 8. 可靠性与可观测性

### 当前事实

- HTTP Server 配置 10 秒 Read/Write Timeout、1 MiB Header；关闭超时 5 秒。
- `GET /health` 只证明 HTTP 进程可响应，不检查 Mongo readiness，也没有独立 liveness/readiness。
- Mongo 客户端只在 Connect + Ping 成功且初始化预算未耗尽、Store未关闭时发布；初始化失败可重试，关闭后不重开。
  初始化锁外执行，同轮合并；独立初始化预算来自MONGODB_TIMEOUT（默认5s），单个请求取消不取消共享初始化。
  Query在等待初始化前建立预算，等待与Find共用MONGODB_TIMEOUT及上游更早截止时间，禁止连接后重新计时。
  Close通知等待者并取消初始化，按调用Context返回；迟到客户端独立5s清理且不发布，重复Close等待同一清理结果。
- 限流是单进程、按 IP 的内存计数器；RATE_LIMIT_REQUESTS 默认 5000，RATE_LIMIT_WINDOW 默认 1s。
  每 IP 独立固定窗口从首次放行起计时，到期重置；lastSeen 仅用于清理，不能让后续放行推迟窗口重置。
  不宣称任意滑动区间都不超过额度；相邻窗口边界可出现突发。
- 业务 Recovery 位于请求日志内部，使业务 Panic 恢复后仍生成一次完成日志；外层 Recovery 保护中间件自身。
  未写响应的 Panic 保持 500 空体、无业务码，success/charge=0/0；已写响应按实际内容记录，不追加错误体。
- NewRateLimiter 仅初始化；bootstrap 显式 Start 并在 HTTP 排空后 Close(ctx)，等待清理 worker 退出。
  SetupRoute 返回的关闭函数由调用方持有；构造函数不得隐式启动不可关闭的后台任务。
  次数和窗口必须为正，非法配置拒绝启动，变更后重启；多实例不共享状态。
- 使用 Logrus；生产 JSON、非生产文本。没有确认 Metrics、Tracing、告警或稳定 Request ID。

### 规则

- 外部 I/O 必须有请求级 timeout；重试仅用于可安全重试操作，并限定次数、退避和总时限。
- 不在 Handler 与底层同时无协调重试，避免重试放大；新增熔断/降级必须通过 Spec。
- 写操作或消息处理新增前明确幂等键、重复处理和一致性；当前 Loan 是只读查询但签名可重放。
- Graceful Shutdown 应先停止接收/排空请求，再关闭其依赖；变更时测试超时和强制退出路径。
- Run 统一处理服务退出/信号并归还错误；禁止服务 goroutine Fatal。HTTP Shutdown 失败尝试 Close，
  Mongo/Redis 清理仍执行；HTTP/依赖各 5s，退出等待另有 5s 保护，部署宽限时间需环境确认。
- Mongo Store 关闭不触发连接且关闭后不重开；Redis 每个读取器负责自己的池，REDIS_POOL_SIZE 不是应用总上限。
- Health、readiness 和 liveness 的语义必须分开记录；不能把简单 `/health` 宣称为依赖就绪检查。
- 请求日志包含方法、路径、HTTP 状态、业务码、耗时、input 和 response_data；完整载荷规则按上述授权例外执行。
- 完成日志和 Handler 诊断日志统一通过服务端生成的 sid 关联，不从客户端读取 ID，
  不生成或输出 request_id，不新增响应字段/Header。未查得不输出异常日志；产品映射异常 WARN，
  其他处理故障 ERROR，并使用固定 error_kind，不输出原始驱动错误或查询值。
- 完成/诊断日志读取 Context sid；SID 中间件生成失败日志保留安全分类，sid 为空。
  health、预检、404、限流拒绝不生成业务 SID，也没有备用关联标识，不伪造 SID 或改变响应。
- 错误分类至少能区分参数、认证、未查得、依赖失败、超时和数据损坏，但契约变更需先确认。
- 引入指标、Trace 或告警时记录名称、标签基数、采样、隐私和运维所有者，不虚构现有平台。

## 9. 性能规范

### 历史基线

`../knowledge/performance_test_report.md` 记录了 2025-11-15 在 macOS 15.3.2、Go 1.23.3、Apache Bench
下的本地结果：Health 最佳记录 16,852 req/s、对应 P99 49 ms；Loan 在 100 并发记录
7,208 req/s、P99 28 ms、失败 0。150 并发 Health 场景只完成 4,547/10,000 请求并出现连接重置。

这些数据早于当前 Go/toolchain 和部分代码，测试机资源、Mongo 数据规模、日志配置及原始结果完整性
不足以支持生产容量结论。历史“7,000+ QPS / P99 <30ms”不是当前门禁。

2026-10-15 已建立 [进程内合成基线](../knowledge/performance_baseline_20261015.md)，
使用当前认证、缓存和文件日志链路；不含 Redis/Mongo/网络，不替代真实依赖容量基线。

### 规则

- 当前没有经确认的生产 SLO、吞吐、P95/P99 或失败率门槛；真实依赖/生产性能基线待建立。
- 性能变更前先定位指标和瓶颈，不仅凭代码直觉引入缓存、池或异步机制。
- 每次压测记录提交、Go 版本、硬件、Mongo 位置/数据量、日志级别、请求数据、并发、持续时间、
  QPS、平均/P95/P99、失败率和资源利用率。
- 前后对比必须使用相同参数和数据；本地结果不得外推为生产单实例容量。
- 修改日志 body、gzip、Mongo 查询/索引、连接池、限流、并发或序列化路径后重新压测。
- 不擅自限制查询结果/解压数据/映射字段数；新增分页或批处理先评估实测内存/耗时并确认策略。
- 连接池参数必须由并发数据和数据库容量支持；不能把默认 100/10 视为永久合理值。
- 压测有外部副作用，只能在授权的隔离环境执行，不对生产或未知地址发流量。

## 10. 数据规范

### 当前数据事实

- MongoDB持久化查询数据，可选MySQL持久化配置；MySQL Schema 为 pkg/mysql/schema.sql，无迁移框架。
- 核心查询按集合字段 `t` 精确查找，字段 `v` 是 gzip 压缩的六个逗号分隔整数。
- 初始化 CLI 为 `t` 创建唯一升序索引，并重建 `data_20251101_0` 至 `data_20251101_f`，但运行服务
  默认只读取配置指定集合；集合选择/分片规则待确认。
- Redis String gin-finapi:product-map:v1:<pcode> 已接入核心路径，JSON 和 Mongo 数据格式不变。
  旧 Hash 必须显式迁移，不自动写入或回退；内存命中不检查年龄。
  刷新缺失/损坏移除，临时故障保留，禁用替换；持续故障时远端撤销传播无硬上限。
  合并回源使用读取器生命周期Context，预算为REDIS_TIMEOUT加已启用的MYSQL_QUERY_TIMEOUT，等待者独立响应取消。
  三类 LRU 容量独立，回源共用并发上限；不缓存失败，不无界创建刷新 goroutine。
  MYSQL_ENABLED 默认false；true时仅显式Fallback允许Redis缺失/损坏/故障回源MySQL，通用Source链错误即停止。
  REDIS_CACHE_ENABLED 默认 true；false 时逐请求直读，无回填、合并读取或刷新，不提供旧快照兜底。
  开关启动时读取，修改后重启生效；不得因关闭缓存而跳过授权、状态、到期或格式校验。
  管理清理只清本进程L1并隔离旧flight，不能修改Redis/MySQL、绕过并发门或取消已接受请求。
  清理数量只反映清理时刻；并发新请求可立即回填，不能承诺返回时始终为空或跨实例立即撤权。
  内存开关不关闭MySQL回源或同步。MYSQL_SYNC_ENABLED默认true，启动后异步一次、此后默认5m。
  同步仅清理自身清单托管键，禁止SCAN/FLUSH或因读失败发布空快照；完整空库仍会清空托管配置，须审查目标。
  客户/Key撤权有传播延迟，不保证立即一致；加密密钥更换先有迁移/回滚Spec，不直接替换。

### 规则

- 当前 Schema 事实由读取代码、写入工具和已验证数据库共同决定；历史文档不能单独作为权威来源。
- Mongo 集合、字段、类型、压缩格式、索引或变量位置变化属于数据契约变更，必须先建 Spec。
- 数据变更提供可审查脚本、兼容读取、验证、备份和回滚方案；脚本默认不能删除已有数据。
- 查询必须受索引支持并限制返回量；新增列表使用稳定排序和分页，避免全集合扫描。
- 事务需求根据真实多文档一致性确认；当前没有事务，不为形式完整性擅自增加。
- 测试数据与生产数据隔离，禁止复制未脱敏生产数据到仓库或日志。
- 主键、时间字段、软删除、归档和数据保留策略尚未形成项目约定，均为待确认。

## 11. Git 与协作

- 当前历史主要使用 `feat:`、`fix:`、`perf:`、`refactor:`、`docs:`，可继续采用 Conventional
  Commits 风格；这是一项从历史归纳的建议，不代表仓库已有强制校验。
- 一个提交/PR 只处理一个可独立说明和回滚的目标；重构与行为变更尽量分开。
- Bug 修复先写复现测试。说明中记录原因、行为变化、验证命令、契约/数据/部署影响和风险。
- 提交前检查 `git status`、`git diff --check`、最小 diff、格式、静态检查、测试和构建。
- Code Review 检查正确性、可读性、架构边界、安全、性能、测试和文档同步。
- 禁止夹带 IDE、日志、缓存、真实 `.env`、压测结果或本地二进制。
- 分支命名、合并方式、Code Owner 和发布审批未形成可验证约定，标记待确认。
- 未经用户明确要求，不执行 `git add`、Commit、Push、Merge、Tag 或发布。

## 12. AI 开发流程

1. 先读 `AGENTS.md` 和 `doc/README.md`，再按任务加载 Rules、Spec、Knowledge。
2. 修改前读取相关源码、测试、配置和一个相似实现；生成文件和历史文档必须回到代码验证。
3. 判断任务影响：局部、无契约变化的是简单任务；跨模块、契约、数据、依赖、架构、安全、部署或
   性能策略变化的是复杂任务，先按 `spec_guidelines.md` 建 Spec。
4. 不发明业务规则，不擅自改变需求、契约、架构、依赖或部署；不确定项写“待确认”。
5. 发现用户、Spec、Rules、Knowledge 和代码冲突时报告位置、影响和选项，不静默选择。
6. 只做满足目标的最小范围变更，保留用户已有修改，不清理未经确认的遗留代码。
7. Bug 修复遵循：复现 → 失败测试 → 根因分析 → 最小修复 → 相关测试 → 全量回归。
8. 所有结论用测试、构建、静态检查或可重复命令验证；未运行或失败必须如实报告。
9. 不删除测试、吞掉错误、硬编码结果或降低检查标准来制造通过。
10. 代码、配置、测试和仓库文档是长期来源；聊天记录不是，重要事实必须同步回仓库。
