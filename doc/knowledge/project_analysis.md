# Gin FinAPI 项目现状分析

> 2026-10-14：限流器构造不再启动 goroutine；bootstrap 显式 Start，并在 HTTP 排空后关闭限流 worker。
> SetupRoute 返回关闭函数，直接调用方须负责清理。phone 转换纯函数已迁到 app/services/phone_lookup.go，
> Handler 在 SID 前调用，32 位截前 20 位、64 位分支及错误码不变；下文 Handler 持有规则的描述为历史结构。
> 详见 [限流生命周期与查询职责](../specs/20261014-限流生命周期与查询职责-设计.md)。

> 2026-10-13：按用户确认，请求日志记录完整 Body/input 与实际响应/response_data（包括请求凭据），
> 不含 Header/Query；status 为业务码，time 为 Unix 毫秒整数，无顶层 msg。队列增加默认 64 MiB 字节预算。
> 超限 Body 只记录前 64 KiB 并标记截断，不放宽校验；下文“不缓存 Body”为历史方案。
> 详见 [完整请求日志](../specs/20261013-完整请求日志-设计.md)，敏感日志权限及保留周期须由运维确认。

> 2026-10-13：所有 32 位 phone 字符串统一取前 20 位查询 t，不再判断十六进制；
> 64 位分支不变。下文 32 位非十六进制原值查询描述属于历史行为。

> 2026-10-13：标准环境配置的请求完成日志默认异步追加 request.log，可切换为独立 charge Redis。
> Redis RPUSH request_log，失败最多尝试三次后丢弃；队列满丢弃，不阻塞响应。消费任务未实现。
> 详见 [异步请求日志](../specs/20261013-异步请求日志-设计.md)，不保证可靠或恰好一次计费记录。

> 2026-10-13：请求完成日志增加 success、charge 整数标记；0 为 1/1、3001 为 1/0，
> 其他或无业务状态为 0/0。状态来自统一 JSON 响应出口，不缓存 Body，不执行扣费。

> 2026-10-13：产品查询编排迁入 app/services/product_query.go，保留原函数和错误语义；
> 后续 app 下新增目录须先获用户明确确认。

> 2026-10-13：phone 在读取凭据前限制为 32/64 字节，其他非空长度为 400/1002；
> 下文任意非空/20 位标识兼容描述为历史记录。仅限制长度，不额外强制十六进制。
> 详见 [phone 长度校验设计](../specs/20261013-phone长度校验-设计.md)。

> 2026-10-12：已删除无引用的 `app/repositories/loan_repository.go`；下文 Repository 存在性描述为历史记录，模型保留。

> 初次分析：2026-08-03；本轮统一缓存、产品 KV 和生命周期复核：2026-10-12

2026-10-12 nonce 签名更新：JSON nonce 必填且为非空字符串，参与 apikey/nonce/timestamp
排序 MD5；旧四字段或五字段签名不再接受，不增加防重放去重。当前凭据来自 Redis，非历史章节所述的 SECRET_KEY。
具体契约及验证见 [nonce 签名设计](../specs/20261012-nonce签名-设计.md)。
> 分析分支：`main`
> 历史审计基线：`85e21d3`。2026-10-11 已复核 phone 原值查询、Mongo 同包拆分及 Redis 产品映射；其他历史审计未全量复核。

## 2026-10-12 隔离集成回归

新增 `tests/integration/run.sh`，在临时 Redis 7.4.7 / MongoDB 8.0.16 容器中验证完整 Loan 链路：
六变量 gzip 数据映射、缓存/直读授权撤销行为、授权缺失/禁用/到期、损坏记录和错误 Redis 类型。
旧 Hash 到 String 迁移验证了源保留、重复执行不覆盖、无效源拒绝。新增离线认证短路和并发读取/刷新隔离测试。
运行说明见 [集成回归](../../tests/integration/README.md)。

上述测试不使用现有开发数据，也未执行生产配置迁移。下文“真实集成未验证”属于历史状态；
真实断线恢复、生产鉴权/TLS、容量和线上客户端联调仍未验证。集成回归尚未纳入 CI。

## 2026-10-11 API Key 存储变更

LoanAuth 已改为每次 GET 独立 Redis String 凭据，使用每 Key secret 验签，支持禁用及 Unix 秒到期。
移除 API_KEY/SECRET_KEY 配置读取，无静态回退或账号层；产品授权按 API Key+pcode 校验并做进程内缓存。损坏记录与 Redis 故障拒绝为 503/9002。
真实数据迁移与 Redis/Mongo 集成尚未验证。下文较早静态认证描述以本节和最新 Spec 为准。

## 1. 分析结论

### 2026-10-11 P1/P2 架构边界修复

本节覆盖下文较早的全局 Mongo、Handler 直连存储和启动 Fatal 描述。
事实来源：bootstrap/app.go、lifecycle.go、route.go、app/services/product_query.go、app/auth/credential.go、
pkg/mongo/store.go、pkg/redis/client.go，以及对应测试和 CI。

- main 只管理信号与退出码，Run 统一启动/服务错误/清理；日志不直接输出底层错误。
- HTTP 与依赖分别使用独立 5s Context；Shutdown 失败尝试 Close，后续 Mongo/Redis 清理不跳过。
  服务退出等待有额外 5s 保护，不是严格 10s 硬上限，生产终止宽限时间待确认。
- 主链路使用 Mongo Store 的实例配置、客户端和超时，关闭未初始化实例不连接，关闭后拒绝重开。
  用户确认后已移除旧 GetMongo/GetClient/包级 Close，查询和关闭仅通过 Store 实例。
- app/services 负责加载映射后查询数据；装配适配层转换存储错误，Handler 只依赖业务语义。
  app/auth 定义凭据与认证错误，中间件不直接依赖 Redis 类型。
- Redis 统一客户端选项，认证/产品映射各自拥有一个池；PoolSize 分别作用于每池，不是全应用上限。
- Redis CLI 保持单文件运行，CI 显式覆盖独立 vet、竞态测试和编译；不运行或写入外部数据。
- 没有新增依赖；凭据、授权和映射统一缓存，每五分钟后台刷新。产品映射改为 String KV，发布前须显式迁移旧 Hash，本轮未运行迁移。实际 Redis/Mongo 集成及容量未验证。

Gin FinAPI 当前是一个体量较小、调用链直接的 Go/Gin API 查询服务。项目可以正常编译，已有单元测试、竞态检测和静态检查均通过，但业务测试覆盖不足，安全、错误治理、并发安全和模块边界仍有明显缺口。

当前实现更适合作为可继续迭代的功能原型。在完成密钥管理、认证校验、日志脱敏、MongoDB 故障恢复和业务测试补齐之前，不建议直接作为高流量生产服务部署。

## 2. 项目概况

### 2.1 技术栈

| 类别 | 当前实现 |
| --- | --- |
| 开发语言 | Go 1.24 |
| HTTP 框架 | Gin 1.11.0 |
| 数据库 | MongoDB Driver 1.17.2 |
| Redis | go-redis 9.7.0，按 pcode 从 String 产品 KV 读取变量映射 |
| 日志 | Logrus 1.8.1 |
| 配置 | godotenv + 环境变量 |
| RPC | 当前未引入 gRPC Server 或 Proto IDL |

Go 版本以 `go.mod` 的 1.24.0、toolchain 1.24.10 为准；旧 README 的“Go 1.18+”已修正。

### 2.2 项目规模

- 项目是单 Go module、单进程 HTTP 服务。
- 对外 HTTP 接口仅有 `GET /health` 和 `POST /api`。
- 核心业务集中在 Loan Handler、配置读取层和 MongoDB 查询模块。
- 未进入调用链的 Model、Repository、旧 Redis 封装和 HelloWorld Proto 已删除。

### 2.3 核心调用链

```text
POST /api
  → Gin 全局中间件
  → LoanAuth：单次 JSON/类型/配置 API Key/时间戳/认证字段签名校验
  → 注入式 LoanHandler：MD5 截前 20 位，旧查询标识兼容
  → app/configdata 内存优先，未命中 GET 产品 KV：加载 JSON 变量映射
  → MongoDB 按 phone 查询字段 t
  → gzip 解压字段 v
  → 解析逗号分隔数据
  → 按产品映射生成变量结果（无硬编码回退）
  → 返回 JSON 响应
```

## 3. 代码结构

| 目录或文件 | 职责 | 当前状态 |
| --- | --- | --- |
| `main.go` | 信号、Run 和退出码 | 已接入 |
| `bootstrap/` | 应用依赖装配、HTTP 生命周期、资源关闭和路由注册 | 已接入 |
| `app/services/`、`app/auth/` | 存储无关的查询流程与认证语义 | 已接入 |
| `routes/` | 健康检查和贷款路由 | 已接入 |
| `app/Http/controllers/loan/` | phone 归一化、SID、注入查询和 HTTP 响应映射 | SHA-256 转换为 TODO，返回 503/9002 |
| `app/middleware/` | CORS、日志、限流、认证和产品授权 | 已接入 |
| `pkg/config/` | 应用配置 | 已接入；主程序执行必填校验 |
| `pkg/mongo/` | Mongo 连接池、查询、解压和结果映射 | 已接入 |
| `pkg/redis/` | 产品变量映射、凭据及产品授权读取器 | 已接入 |
| `pkg/rescode/` | 业务错误码 | 已接入 |
| `pkg/utils/` | SID、时间戳和签名工具 | 已接入 |

## 4. 验证基线

本次分析执行了以下命令：

```bash
go test ./...
go test -race ./...
go vet ./...
```

验证结果均为通过。

当前自动化测试主要覆盖：

- SID 格式、长度、并发唯一性及随机源失败；
- 时间戳与签名生成、校验；
- 错误码消息；
- Mongo 客户端初始化重试、并发发布、关闭后重建、错误分类和数据完整性；
- Loan 参数、成功及错误响应映射；
- 配置环境读取、默认值和必填校验；
- Health 路由、CORS、限流和主程序关闭顺序。

以下关键模块目前仍没有测试：

- 真实 MongoDB 的查询、连接超时、运行期断线和恢复集成行为；
- 真实 Redis 连接、认证、断线恢复集成行为（读取器、JSON 校验与离线 HTTP 链路已有测试）；
- 完整部署环境中的日志采集和敏感信息保护；Logger 元数据及 Handler/认证组件已有自动化测试；
- 隔离环境中的端到端请求。

2026-09-12 的修复已用并发测试和 `go test -race` 验证 SID 与 Mongo 客户端初始化路径；这不能
替代真实 Mongo 故障场景的集成验证。

## 5. 主要风险

### 5.1 已整改：签名密钥配置化

签名密钥已改为通过 `.env` 的 `SECRET_KEY` 加载，并在应用启动时执行必填校验。生产环境仍应通过密钥管理系统注入强随机值，避免直接使用示例值；若 API Key 对应不同客户，后续还应改为按客户取得对应密钥。

### 5.2 已整改：请求日志敏感数据捕获

位置：`app/middleware/logger.go`

2026-10-11 Logger 已停止读取、缓存和记录请求/响应体，只记录方法、路由模板、HTTP 状态码和耗时。
LoanHandler 仅记录错误操作，不输出可能含凭据或查询内容的驱动错误。以下为已移除行为的历史风险：

- phone；
- apikey；
- sign；
- 查询结果数据。

同一成功请求的请求体还会分别在开始和完成日志中记录两次。

风险：

- 个人信息和凭据泄露；
- 大请求造成额外内存占用；
- 高 QPS 下形成严重日志和序列化开销；
- 响应数据量增长后可能放大磁盘和日志采集成本。

建议：默认只记录路径、耗时、HTTP 状态、业务码和 SID；对 phone、apikey、sign 做删除或掩码处理；增加 body 大小上限和可配置采样率。

### 5.3 部分整改：配置 API Key 鉴权

2026-10-11 `/api` 已通过 Redis 凭据按 API Key 读取独立 secret，并校验状态、到期时间、签名和产品授权。
旧 `AuthRequired`、`APIKeyAuth` 占位实现已删除。

建议：明确认证模型，在签名验证前查询 API Key 对应的主体、状态和密钥；认证失败统一返回认证错误，不进入 MongoDB 查询。

### 5.4 已整改：SID 随机源并发不安全

位置：`pkg/utils/sid.go`

SID 已改用 `crypto/rand` 和拒绝采样；随机源错误向上传播，Loan Handler 会返回稳定内部错误。
已增加 200 个 goroutine 的并发唯一性及竞态测试。

剩余边界：SID 仍不作为认证或幂等凭据，不能据此推导安全唯一性承诺。

### 5.5 已整改：MongoDB 首次初始化失败后无法恢复

位置：`pkg/mongo/client.go`（2026-10-11 从 mongo.go 同包搬移）

Mongo 客户端改为互斥保护的生命周期管理器，只有 Connect + Ping 成功才缓存；首次失败可重试，
并发初始化只发布一个客户端；当前 Store 关闭后禁止重新初始化。Ping 失败会尝试释放候选客户端。

剩余边界：尚未增加启动期 Mongo readiness 和真实断线恢复集成测试。

2026-10-11 已按同包职责拆分：store.go 管理实例配置和查询，mongo.go 定义 BSON 类型及超时解析，client.go 管理连接，
codec.go 解压 gzip、校验 CSV 并映射变量，errors.go 定义和分类错误。
BSON t/v、gzip/CSV、查询错误和超时契约不变；按用户要求删除旧全局导出入口及未使用的 connectToMongoDB。
新增离线特征测试覆盖实际 BSON 查询和文档类型、完整六变量映射及异常数据；不代表真实 Mongo 集成验证。

2026-10-12 产品映射改为 Redis String `gin-finapi:product-map:v1:<pcode>`，value 为非空 JSON 对象，
变量名对应非负整数 CSV 下标。与凭据、授权统一内存优先，未命中读 Redis，每五分钟后台刷新。
无缓存年龄判断；状态和业务到期逐次检查。临时故障保留，明确删除/损坏移除，不回写或硬编码回退。
`REDIS_TIMEOUT` 默认 1s，LRU 容量默认 1000/10000/1000，共用回源并发默认 4；未做生产容量验证。
`REDIS_CACHE_ENABLED` 默认 true；false 时三类配置每请求直读 Redis，不回填、不合并、不启动刷新，重启生效。
缺失、重复键、空对象或无效 JSON 返回 HTTP 200/6001；Redis 故障或读取超时返回 HTTP 503/9002，
这些分支不访问 Mongo。CSV 长度仍须等于最大下标加一，BSON t/v 和 gzip 格式不变。
关闭顺序为 HTTP 排空、缓存 worker/回源取消及等待、Mongo Store、Redis 池。
迁移工具按产品 HGET 旧 Hash → 校验 → SET NX 新键，保留源和已有目标；本轮未连接实际 Redis。

### 5.6 已整改：核心 Mongo 错误分类及对外泄露

Mongo 边界已提供可判定的未查得、不可用和数据损坏错误，Context 超时/取消保持原因；Loan
Handler 按已确认 Spec 映射 HTTP/业务码，并仅返回 `rescode` 稳定文案。

已固定：未查得 200/3001、超时 504/9003、不可用 503/9002、数据损坏 500/9001。其他
Handler 的错误响应仍需在相关需求中逐项审计。

### 5.7 部分整改：启动配置校验

主程序现已在加载配置后调用 `AppConfig.Validate()`，MongoDB DSN、数据库名或签名密钥缺失时会终止启动。`config.Load()` 当前仍永远返回 `nil`，配置加载和校验分为两个调用步骤。

建议后续进一步完成 MongoDB 连接初始化，确保关键依赖不可用时在启动阶段明确失败。

### 5.8 已整改：LoanHandler 依赖显式化

2026-10-11 请求 DTO 移到 pkg/request，解析/认证由 LoanAuth 执行。
LoanHandler 通过构造函数注入查询、SID 和 Logger，bootstrap.NewRouter 装配 Mongo 查询闭包、认证和限流，main 只调用入口。
不读取全局配置；HTTP 错误映射仍引用 Mongo 错误分类。以下为历史职责和未来参考，不代表当前实现：

原 Controller 同时负责：

- 请求 DTO；
- 类型转换和参数校验；
- API Key 检查；
- 时间戳和签名验证；
- 环境配置读取；
- MongoDB 数据访问；
- 错误映射；
- HTTP 响应构造。

这使得业务逻辑难以独立测试，也使已有的模型和 Repository 层失去作用。

本轮为测试注入查询和 SID 函数，并使用明确请求 DTO，但没有为单一调用链增加空壳层。若业务编排
继续增长，可评估拆分为：

```text
HTTP Handler
  → Request Validator / Authenticator
  → Loan Service
  → Loan Repository
  → MongoDB
```

### 5.9 已整改：配置入口重复

MongoDB 配置已统一从 `pkg/config` 读取并注入 Store 配置快照；旧 `pkg/env`、`mongo.Config`、
`GetMongo` 和兼容 Getter 已删除。

### 5.10 已整改：未使用或半成品模块

旧 Redis 通用封装、LoanRepository、LoanRequest Model、HelloWorld Proto、旧 Mongo 连接实现和占位认证中间件均已删除；
当前目录只保留已进入应用、测试或独立运维 CLI 的实现。

## 6. 性能与可观测性判断

仓库已有历史压测报告，但结果受测试机器、日志量、MongoDB 数据规模和连接配置影响，不能直接作为当前生产容量承诺。

当前最明显的性能风险不是 Gin 本身，而是：

- 请求 Body 已限为 64 KiB，完整请求/响应日志捕获已移除；
- 高 QPS 下日志写入量过大；
- MongoDB 查询已有 MONGODB_TIMEOUT；真实数据库索引和容量仍需环境验证；
- gzip 解压结果没有大小上限。

当前日志也缺少稳定的 request ID/SID 关联、错误类型、MongoDB 耗时和结果类型等字段。建议在压测前先调整日志策略，再重新建立容量基线。

## 7. 文档与仓库一致性

以下是历史不一致清单，未全量复核；Go 版本、当前鉴权和 Redis 映射描述已同步 README：

- README 写 Go 1.18+，实际要求 Go 1.24；
- README 包含 Docker 构建和运行说明，仓库没有 Dockerfile；
- 历史 README 宣称 Redis 能力而未启用；当前已实际用于产品映射，连接池和读超时均配置化；
- README 中的认证示例没有反映 `/api` 当前真实注册方式；
- 项目根目录已有 `AGENTS.md`，并以 `doc/README.md` 作为规则、Spec 和 Knowledge 索引；
- 仓库跟踪了约 27 MB 的 macOS arm64 编译产物 `main`，增加仓库体积且不具备跨平台复用价值；约 2.7 MB 的 `access.log` 已被忽略但会污染本地工作区。

### 7.1 AI Coding 文档体系

2026-08-03 盘点时，仓库不存在 `docs/`、`docs/specs/` 或 `docs/knowledges/`，只有既有的
扁平 `doc/` 目录。当时在根目录增加 `AGENTS.md` 和文档规则；后续文档已按
`doc/rules/`、`doc/specs/` 和 `doc/knowledge/` 迁移。当前入口为：

- `../README.md`：Rules、Specs、Knowledge 总索引和冲突处理；
- `../rules/project_rules.md`：长期项目规则；
- `../rules/spec_guidelines.md`：Spec 命名、模板和生命周期；
- `README.md`：现有知识文件索引和加载规则。

### 7.2 现状审计清单

| 优先级 | 问题与证据 | 影响 | 建议 |
| --- | --- | --- | --- |
| 已整改 | Logger 只记录安全元数据；LoanAuth 限制 Body 为 64 KiB | 已移除完整请求/响应捕获 | 保持日志隐私测试；生产采集链路另行验证 |
| 部分整改 | LoanAuth 校验配置 API Key、时间戳与原值签名 | 单账号可认证，仍无多账号状态与权限体系 | 保留原查询标识契约；多账号方案另行确认 |
| 已整改 | SID 改用 `crypto/rand`，并有随机失败、并发和 race 测试 | 原共享随机源竞态已消除 | 保持测试；不得降级为可预测随机源 |
| 已整改 | Mongo 初始化仅发布成功客户端，失败可重试、关闭可重建 | 原首次失败固化风险已消除 | 后续补真实故障集成测试和 readiness |
| 已整改 | Loan 区分未查得、超时、不可用和数据损坏，响应不含原始错误 | 客户端和监控可按稳定契约分类 | 保持契约测试，变更前先建 Spec |
| 部分整改 | Controller、路由、中间件、Mongo、Redis 映射、配置和主程序已有直接测试；真实 Redis/Mongo 集成仍缺失 | 核心纯逻辑与离线 HTTP 链路可回归，外部集成仍有盲区 | 在隔离环境补依赖故障和真实集成测试 |
| 已整改 | `pcode`、`timestamp` 使用严格 JSON 整数解析 | 小数、字符串、指数和溢出不再被静默接受 | 保持边界测试 |
| P1 | `cmd/init_mongo.go` 对 16 个同名集合执行 `Drop` | 误用会造成数据丢失 | 仅授权测试环境运行，增加目标确认与回滚方案 |
| 已整改 | 主调用链统一到 `pkg/config`，旧 `pkg/env` 和兼容 Getter 已删除 | 配置入口单一 | 新代码继续使用启动时加载的配置快照 |
| 已整改 | 旧 Redis 封装、Repository/Model、HelloWorld Proto、旧 Mongo 连接和占位认证均已删除 | 降低维护成本和能力误判 | 新能力按真实需求和 Spec 引入 |
| P2 | `README.md` 的 Go 1.18+、JWT/API Key、Redis、Docker 和限流描述与代码不一致 | 新成员和 AI 使用错误事实 | 单独审查 README；本次只记录，不擅自改产品说明 |
| 部分整改 | 已引入最小 CI（`.github/workflows/ci.yml`：gofmt、vet、`test -race`、build），仍无项目级 Lint、漏洞扫描、覆盖率门槛或部署清单 | 格式、编译、竞态与构建回归可自动拦截；更细的质量门禁依赖人工 | 按需通过 Spec 补 golangci-lint、覆盖率门槛与漏洞扫描 |
| 部分整改 | 约 27 MB 的本机 Mach-O `main` 已移出 Git 跟踪，`.gitignore` 增加 `/main` | 新提交不再累积该二进制；历史中的 blob 仍占据仓库体积 | 如需缩减历史体积，另行评估 history filter 与强推影响 |
| P2 | `/health` 只返回 `ok`，无 readiness/liveness、Metrics、Tracing 或 Request ID | 依赖故障不可见，排障关联不足 | 先定义运维语义和平台，再补可观测能力 |
| P2 | 历史 `tests/bench/bench.sh` 请求体只有 phone，与当前 Loan 必填字段不兼容 | 脚本当前不能有效压测成功业务路径 | 在性能 Spec 中修订测试数据生成与安全注入 |

## 8. 建议实施路线

### 阶段一：安全与正确性

1. 为不同环境配置独立的强随机签名密钥，并建立轮换机制。
2. 已完成单个配置 API Key 校验；多账号来源与独立 secret 留待未来方案。
3. 已完成日志移除完整 Body 捕获和 Loan Body 限制。
4. 已完成：SID 改用并发安全实现。
5. 已完成：区分业务未查得和系统异常，停止向客户端返回底层错误。

### 阶段二：稳定性

1. 启动时校验 MongoDB 配置并完成连接初始化。
2. 已完成：重构 Mongo 客户端生命周期，解决首次失败后无法恢复的问题。
3. 为 MongoDB 查询增加独立超时。
4. 已完成数据字段完整性校验；gzip 解压大小上限仍待设计。
5. 已完成本轮 Mongo 相关 HTTP 状态码与业务错误码规范。

### 阶段三：结构治理

1. 已完成：使用明确的 Loan 请求 DTO 和严格整数类型。
2. 已完成 LoanAuth 与注入式 LoanHandler；出现实际多步骤业务编排时再评估 Service。
3. 现有 QueryFunc 已隔离 Mongo 访问；未使用的 Repository CRUD 接口已删除。
4. 主调用链已统一使用 `pkg/config`，旧 `pkg/env` 和兼容 Getter 已删除。
5. 旧 Redis 封装、HelloWorld Proto、旧 Mongo 连接代码和占位认证模块已清理，已启用的产品映射保留。

### 阶段四：测试与交付

1. 已完成：为 Controller 增加表驱动参数校验测试。
2. 已完成：使用 `httptest` 覆盖 `/health`、`/api` 和相关中间件边界。
3. 已完成异常数据单元测试；真实 MongoDB 集成测试仍待隔离环境。
4. 已完成：增加 SID 并发测试和限流边界测试。
5. 生成覆盖率报告并设定最低门槛。
6. 补齐 Dockerfile、健康检查及部署说明。
7. 已增加 `AGENTS.md` 和文档索引；项目 README 的历史失实内容仍待单独审查。

## 9. 建议验收标准

完成上述整改后，建议至少满足：

- 仓库及日志中不存在明文签名密钥、完整 API Key 和完整手机号；
- 非法或未知 API Key 无法通过认证；
- `go test ./...`、`go test -race ./...`、`go vet ./...` 全部通过；
- `/api` 的参数、认证、签名、未查得、数据库异常和成功路径均有自动化测试；
- MongoDB 启动失败和运行期故障具有明确行为及可观测指标；
- Controller 不直接读取环境变量或操作 MongoDB；
- README、Go 版本、Docker 部署说明和实际代码保持一致；
- 重新执行压测并记录测试环境、数据规模、日志配置、P95/P99 和错误率。

## 10. 总体评价

项目的优点是规模小、核心流程清晰、依赖数量可控，且已有基础测试和优雅关闭机制，重构成本相对较低。

当前主要短板集中在安全边界、异常语义、并发安全、配置生命周期和业务测试覆盖。建议先解决 P0/P1 风险，再进行分层重构和性能优化，避免在不可靠的基线上继续增加功能。

## 2026-10-11 请求加密边界

同日限流配置化：main 将 `config.RateLimitConfig` 传给 bootstrap；环境变量
RATE_LIMIT_REQUESTS/RATE_LIMIT_WINDOW 默认 5000/1s，非法值拒绝加载，修改后重启。
原按 IP、单进程算法及后台清理生命周期不变。

phone 是非空 JSON 字符串，原完整值用于签名；MD5（32 位十六进制）截前 20 位查 t。
旧 20 位及其他历史非空标识原样兼容；SHA-256（64 位十六进制）留转换服务 TODO，当前 503/9002，
不生成 SID 或查库。不得对 SHA-256 字符串再次 MD5 冒充原手机号 MD5。
原 AES 强制解密方案已按用户澄清撤销，PHONE_AES_KEY 不再读取或必填。
认证 API_KEY/SECRET_KEY、注入式 LoanHandler、安全日志和限流配置保留。
用户示例及 32/64 字符标识已有原值查询回归；真实 Mongo/客户端联调仍未执行。
详见[设计](../specs/20261011-请求加密鉴权-设计.md)。
