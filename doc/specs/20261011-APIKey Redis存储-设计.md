# API Key Redis 存储：设计

> 状态：凭据认证和产品授权已实施。2026-10-11；缓存策略于 2026-10-12 被[统一缓存设计](20261011-Redis内存缓存-设计.md)替代。
> 下文每请求 GET 是历史阶段说明；当前路由使用统一内存优先读取，不按缓存年龄回源。
> 2026-10-12 当前仅 apikey、nonce、timestamp 参与签名，旧四字段或五字段签名不再接受，见 [nonce 签名设计](20261012-nonce签名-设计.md)。
> 本文同时记录设计与 2026-10-11 实施事实；更晚的用户确认覆盖本文早期“产品授权暂缓”范围。

## 当前实现与问题

事实来源：`app/middleware/loan_auth.go`、`pkg/config/app_config.go`、
`pkg/redis/product_map.go`、`routes/api.go`、`pkg/rescode/rescode.go`。

变更前 LoanAuth 使用配置 API_KEY 比对请求体 apikey，再使用全局 SECRET_KEY 验签；
配置加载要求二者非空。Redis 已用于 mongo_map 产品映射，但未保存认证账号。
现有中间件单次解析 JSON，成功后传递已认证请求给 LoanHandler。
历史 20260803 账号设计为尚未实施的三层缓存草案，其 HTTP 200 拒绝策略、运营中心、
MySQL 与当前代码不同，本方案不沿用这些假设。

## 目标、范围与假设

建议将 API Key 认证信息迁至 Redis，支持多个 Key、独立 secret、禁用和可选到期时间。
假设 Redis 是首期认证记录的直接来源，由受控运维写入；实际数据维护方待确认。
保持现有 JSON 字段、签名算法/参数、时间戳窗口、phone 查询和产品映射契约。
不增加 MySQL、运营中心同步、API Key 凭据缓存、配额或 HTTP 管理接口。产品授权在后续确认后纳入本轮。
API Key 是不透明字符串；不 trim、不改大小写；合法旧 Key 的长度需在迁移前确认。

## 方案及备选方案

已确认采用每账号一个 Redis String Key：

```text
Key: gin-finapi:auth:apikey:v1:<sha256(apikey)>
Value: 完整账号 JSON
```

摘要为原始 apikey 字节的 SHA-256 小写十六进制字符串；Key 中不保存明文凭据。
每次鉴权只执行一次 GET，不扫描账号集合，不自动写入，不缓存正向或负向结果。
摘要不是密码加密，也不改变请求签名算法。
账号完整替换使用一次 SET，避免多字段更新产生半条记录。

考虑后续上万或更多账号，独立 Key 可在 Redis Cluster 中分散到不同槽位，
也便于单账号删除、过期和维护；实际单机 Redis 不会因此自动分片。
代价是比一个大 Hash 有更多 Key 内存开销。容量需按记录大小、QPS 与实际部署压测，
不能仅按账号数量承诺吞吐，也不因规模预期增加 Cluster 部署组件。
单个大 Hash 作为备选，优点是通常节省内存，但整个 Hash 在 Cluster 中仍属于一个槽位。

expires_at 始终由程序校验。0 表示永不过期，此时不设 TTL；非零值可在写入时
通过同一 SET 命令附带 EXAT，使 TTL 仅用于辅助清理，不能替代业务时间判断。
更新记录必须同时更新或移除旧 TTL，禁止先 SET 后 EXPIRE 的非原子写入。
已到期账号不得因更新工具重设 TTL 而恢复有效。
不使用全局 secret 配合 Key 白名单：独立 secret 可以隔离客户轮换影响。

## 数据与迁移

示例只有占位值，不可作为真实初始化数据：

```json
{
  "version": 1,
  "secret": "<injected-secret>",
  "status": 1,
  "expires_at": 0
}
```

| 字段 | 建议约束 |
| --- | --- |
| version | 必填整数，严格为 1 |
| secret | 必填，1–256 字节，不截断、不 trim |
| status | 必填整数，仅允许 0（禁用）或 1（启用） |
| expires_at | 必填非负 Unix 秒；0 永不过期；now >= expires_at 时拒绝 |

解析拒绝重复字段、未知字段、多个 JSON 值、null 和错误类型；记录最大 4 KiB，
读取后先检查大小再解析。GET 本身已接收完整值，该上限不替代写入端与 Redis 容量控制。
不保存明文 apikey，不把 secret 做单向哈希：现有验签需要原始 secret。

迁移前先确认目标 Redis 地址/DB、维护方、备份策略与旧凭据长度（不输出凭据）。
将原 API_KEY 的摘要与原 SECRET_KEY 写成启用记录，使用 SET NX 防止覆盖已有账号；
存在记录时停止并报告冲突，不能按“存在”直接宣称迁移成功。
迁移工具使用显式环境注入，默认 dry-run，只展示目标/记录数量，不展示密钥或摘要。
本设计不执行任何写入或初始化工具。

生产 Redis 作为配置存储必须明确持久化、恢复和避免账号被淘汰的措施；
丢失账号记录会拒绝请求，Redis 不可用会使全部 Loan 鉴权失败。
若当前 Redis 仅按可丢失缓存运营，应先解决配置持久性或重新选择权威数据源。

## 接口与兼容性

LoanAuth 注入小型读取函数，不在中间件创建客户端：

```go
func LoanAuth(load func(context.Context, string) (redis.APIKeyCredential, error)) gin.HandlerFunc
```

实现位于 `pkg/redis/apikey_store.go`，类型和构造函数遵循现有 ProductMaps 风格；
测试在同包 `apikey_store_test.go`。中间件通过 fake reader 做 httptest，保留单次 Body 解析。
应用装配处创建读取器、注入路由，并在 HTTP 排空后关闭其连接池；不使用旧全局 Redis 工具，
避免 Background Context 和错误转空值。首期独立连接池便于资源所有权明确；
若要共用 ProductMaps 客户端，需要另行评估生命周期，不夹带该重构。

流程：现有 JSON/phone/pcode 校验 → apikey 非空 → GET → 校验记录与状态/有效期
→ 原时间戳校验 → 用凭据 secret 验签 → SetAuthenticated → LoanHandler。
每次读取返回完整账号快照；禁用/轮换对更新完成后的读取生效，已读取旧记录的在途请求可能完成。
/health 不读取认证 Redis，仍仅表示 HTTP 进程可响应。

建议切换后配置不再要求 API_KEY、SECRET_KEY；Redis 认证与产品映射共用既有 REDIS_* 配置，
包括 REDIS_TIMEOUT（默认 1s）。旧变量不再参与请求放行，不提供双源兜底。
同步配置校验、.env.example、README、AGENTS/Rules 中相关事实；真实 .env 不提交。

## 错误、超时和可靠性

| 情形 | HTTP / 业务码 | 下游查询 |
| --- | --- | --- |
| apikey 为空、不存在、禁用、到期 | 400 / 2001 | 不查产品映射或 Mongo |
| 记录损坏、格式版本不支持、空 secret 等 | 503 / 9002 | 不查产品映射或 Mongo |
| Redis 超时、网络/权限/类型错误 | 503 / 9002 | 不查产品映射或 Mongo |
| 时间戳或签名错误 | 保留 400 / 1005 或 2002 | 不查产品映射或 Mongo |
| 认证成功 | 继续原 Loan 流程 | 按原契约 |

未知、禁用与到期对外统一文案；内部保留分类，但不泄露账号是否存在。
Redis I/O 继承请求 Context，单次读取使用 REDIS_TIMEOUT，无自动重试、缓存或环境变量回退。
新增 Redis 故障响应是依赖变化所需行为，属于待确认契约扩展；不沿用历史 HTTP 200 草案。

## 安全、性能与可观测性

不记录 API Key、摘要、secret、sign、phone、原 JSON 或带敏感内容的驱动错误。
日志仅记录固定错误类别与安全请求元数据。不为日志增加账号标签或高基数维度。
服务只需 GET 权限；维护账号使用独立写权限。部署需确认认证记录访问边界、传输保护、
持久化备份访问权限；若需要 secret 静态加密，应先确认密钥管理方案，不能靠哈希替代。
首期每次 Loan 增加一次 Redis RTT；未验证吞吐/SLO，不能引用历史压测作为容量保证。
轮换推荐生成新 API Key + 新 secret，允许明确的迁移窗口后禁用旧记录；
同 Key 直接更换 secret 会使使用旧 secret 的请求立即验签失败。

## 验收与验证

- 两个 Key 分别用自己的 secret 成功；交叉 secret 失败。
- 缺失、禁用、到期边界与签名错误返回上述契约，且 Handler 不被调用。
- malformed JSON、重复/缺失字段、未知版本、非法类型/长度与错误 Redis 类型返回 503/9002。
- 区分 redis.Nil 与网络错误；取消与超时传播；每次请求读取，无失败放行。
- 健康接口无变化，既有请求解析与业务响应回归通过。
- 离线 unit/component 使用注入读取函数、fake reader 与可控时钟；
  真实 Redis 集成仅在授权的隔离 Key 命名空间上验证 GET、完整替换、禁用、撤销和故障。
- 执行 gofmt、go vet ./...、go test ./...、go test -race ./...、go build ./...。
  实施已执行离线测试及代码门禁，未验证真实 Redis/Mongo 集成。

## 风险、发布和回滚

顺序：确认模型/契约和维护方 → 编写读取器/失败测试 → 注入中间件与配置变更
→ 同步文档及回归 → 授权后导入旧记录并验证 → 部署切换。
没有默认启动自动导入。缺失 Redis 记录不能通过环境变量兜底。
回滚需部署旧版本并恢复对应环境配置；旧版本不能识别多账号、禁用或轮换状态，
因此先确认回滚是否会重新启用已撤销凭据。Redis 新账号 Key 保留，不自动删除。

## 后续 API Key–产品关系（结构已确认，暂不实施）

产品授权直接关联 API Key，不通过 account_id 或账号产品表。
每个 API Key 与 pcode 的组合使用一个独立 Redis String Key：

```text
Key: gin-finapi:auth:apikey-product:v1:<sha256(apikey)>:<pcode>
Value: {"version":1,"status":1,"expires_at":0}
```

apikey 摘要计算与凭据 Key 一致；pcode 使用校验后的十进制整数文本。
一个 Key 可关联多个产品，同一产品可关联多个 Key；GET 精确查询单条关系，不扫描列表。
凭据 JSON 与产品授权 JSON 分开维护，产品变量映射现由 String 产品 KV 提供，旧 Hash 须显式迁移。
依据用户纠正，凭据不引入账号层，删除此前建议的 account_id，产品授权直接关联 Key。

建议产品记录的 version、status、expires_at 校验及 TTL 辅助清理规则与凭据记录一致。
后续流程建议为：验证凭据及签名 → GET 产品授权 → 查询产品映射及 Mongo。
缺失、禁用、到期授权拒绝；Redis 故障或损坏记录不放行，具体 HTTP/业务码实施前确认。
两次 GET 不保证同一时点快照；授权撤销对之后的读取生效，已经读取的在途请求可能完成。
不因潜在 Cluster 场景添加 hash tag，首期也不依赖跨 Key 事务。
本结构只提供单条授权查询；列出一个 Key 的全部产品若成为需求，再设计索引，不使用 KEYS。
2026-10-11 后续确认启用产品授权：验签成功后必须读取对应 API Key+pcode 授权；缺失、禁用或到期返回 HTTP 403/2004，Redis 故障或损坏记录返回 HTTP 503/9002。上线前必须在 Redis 配置有效授权。

授权信息经统一读取层按需缓存；未命中读 Redis，后台每五分钟刷新，不扫描全库、不判断缓存年龄。
业务到期逐次检查；后台临时故障保留，明确删除/损坏移除，禁用替换。正常变更在下一轮同步完成后生效；持续故障时延迟无硬上限。

## 已确认与待确认项

已确认：每个 API Key 对应一个独立 Redis String Key，使用摘要作为 Key 后缀，
完整凭据 JSON 作为 Value；到期时间程序校验，TTL 辅助清理。
后续 API Key–产品关系也采用每个组合一个独立 String Key，不采用 Hash 或账号间接关联。

其余待确认：

1. 是否采用每 Key 独立 secret、启停状态与可选到期时间的上述最小模型？
2. Redis 记录由人工受控工具维护，还是已有运营平台负责写入？
3. 是否接受 Redis 为必要认证依赖、每次读取、无环境变量回退？
4. 首期是否需要按 pcode 授权？默认本轮不包含；认证通过仍按现有产品逻辑查询。

## 实施记录

- APIKeys.Load 每次 GET 独立 String，严格解析四字段凭据；不缓存、不写入、不回退。
- API Key+pcode 产品授权在签名校验后读取；未命中回源，后台每 5 分钟刷新，请求不按缓存年龄回源。
- LoanAuth 注入读取函数，用对应 secret 验签；装配负责关闭认证与产品映射两个连接池。
- 移除静态 API_KEY/SECRET_KEY 配置读取和必填校验，不添加账号层。
- 未实现运维写入工具、真实数据迁移或部署；需要维护方在切换前准备有效凭据与产品授权记录。
- 验证结果见最终交付说明；离线测试不代表真实 Redis/Mongo E2E。

验证：gofmt 无输出，go vet ./...、go test ./...、go test -race ./...、go build ./...、git diff --check 均通过。
命令使用 GOCACHE=/private/tmp/gin-finapi-go-cache，避免默认缓存目录的沙箱写入限制。

## 初始化样例补充

`cmd/init_redis.go -auth-samples` 已增加显式的测试凭据及产品授权 String KV 样例。
从 INIT_API_KEY/INIT_API_SECRET 读取，SET NX 保留已有值，默认命令仍只写产品映射。
这是测试样例工具，不是上述生产迁移工具；不提供 dry-run、记录正确性核验或生产轮换。
操作和部分写入说明见 cmd/README.md。离线初始化测试（含 race）、单文件 vet 与 diff 检查通过；未运行初始化。
