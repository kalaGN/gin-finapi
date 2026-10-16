# 配置与存储说明

核对日期：2026-10-16。完整配置项与默认示例以 [`.env.example`](../../.env.example) 为入口，
实际解析和校验见 [`pkg/config`](../../pkg/config/config.go)。本文从原README整理，不读取真实.env。

[文档索引](../README.md) · [接口与鉴权](api_reference.md) · [开发、部署与日志](operations.md)

## 配置方式

在仓库根目录复制 `.env.example` 为 `.env`，按目标环境编辑；也可提供等价环境变量。
不得覆盖已有.env或提交真实凭据，修改后重启生效。应用不自动初始化Mongo数据、产品或API Key。
Redis-only为默认模式；数据操作须先确认环境和范围，见 [工具说明](../../cmd/README.md)。
`cmd/init_mongo.go`会删除并重建16个集合，未经明确授权不得运行。

## 管理接口与可选MySQL

- `CONFIG_CACHE_TOKEN`默认空，非空时启用独立Bearer认证的缓存清理接口；不能含空白。
  调用和限制见 [清除本进程配置缓存](api_reference.md#清除本进程配置缓存)。
- `MYSQL_ENABLED`默认false；启用后须设置MYSQL_HOST/DATABASE/USER及连接、TLS和池参数，
  密码来自MYSQL_PASSWORD。MYSQL_TLS_MODE默认verify_identity；远端证书及网络仍需目标环境验证。
- `MYSQL_SECRET_KEY`须为独立随机32字节的Base64，`MYSQL_SECRET_KEY_ID`不可空；
  不可直接替换已有加密密钥，应先设计密文迁移与回滚。不要将真实值写入文档或命令历史。
- 仅MySQL启用时，`MYSQL_SYNC_ENABLED`默认true：启动异步一次，随后默认MYSQL_SYNC_INTERVAL=5m；
  MYSQL_SYNC_TIMEOUT默认30s，会覆盖同名Redis配置。首次启用先检查目标、Schema、初始化和dry-run。
- 四表、回源顺序、一致性、撤权及同步风险见 [MySQL说明](mysql_configuration.md)；
  具体命令见 [MySQL初始化与同步](../../cmd/README.md#mysql-配置初始化与同步)。

## 环境变量说明

下表聚焦运行时通用项；MySQL及管理开关见上节，所有配置名见模板。
REDIS_POOL_SIZE是每个连接池的参数：认证与产品映射各一池，MySQL同步启用时另有发布池，
不等于应用总连接上限；日志使用独立CHARGE_REDIS_*池。

| 变量名 | 说明 | 默认值 | 必填 |
|--------|------|--------|------|
| `SERVER_PORT` | 服务端口 | `8082` | 否 |
| `ENV` | 运行环境 | `production` | 否 |
| `RATE_LIMIT_REQUESTS` | 每个 IP 在窗口内允许的次数，正整数 | `5000` | 否 |
| `RATE_LIMIT_WINDOW` | 限流窗口，正 Go duration，例如 `1s`、`500ms` | `1s` | 否 |
| `MONGODB_DSN` | MongoDB 连接字符串 | - | 是 |
| `MONGODB_DATABASE` | 数据库名称 | - | 是 |
| `MONGODB_COLLECTION` | 集合名称 | `data_20251101_0` | 否 |
| `MONGODB_TIMEOUT` | 初始化等待与Find共用预算 | `5s` | 否 |
| `MONGODB_MAX_POOL` | 最大连接数 | `100` | 否 |
| `MONGODB_MIN_POOL` | 最小连接数 | `10` | 否 |
| `REDIS_ADDR` | Redis 地址 | `localhost:6379` | 否 |
| `REDIS_PASSWORD` | Redis 密码 | (空) | 否 |
| `REDIS_DB` | Redis 数据库 | `0` | 否 |
| `REDIS_POOL_SIZE` | Redis 连接池大小 | `10` | 否 |
| `REDIS_TIMEOUT` | 配置合并回源及单次 Redis 读取超时，正 Go duration | `1s` | 否 |
| `REDIS_CACHE_REFRESH_INTERVAL` | 已缓存项后台刷新周期，非TTL | `5m` | 否 |
| `REDIS_CACHE_ENABLED` | 是否使用三类内存缓存，`true` / `false`，重启生效 | `true` | 否 |
| `REDIS_APIKEY_CACHE_MAX_ENTRIES` | 凭据 LRU 最大条目数，正整数 | `1000` | 否 |
| `REDIS_PRODUCT_AUTH_CACHE_MAX_ENTRIES` | 授权 LRU 最大条目数，正整数 | `10000` | 否 |
| `REDIS_PRODUCT_MAP_CACHE_MAX_ENTRIES` | 映射 LRU 最大条目数，正整数 | `1000` | 否 |
| `REDIS_CACHE_SYNC_CONCURRENCY` | 三类数据共用的回源并发上限，正整数 | `4` | 否 |
| `REQUEST_LOG_OUTPUT` | 请求日志后端，`file` 或 `redis` | `file` | 否 |
| `REQUEST_LOG_FILE` | 文件后端追加路径 | `request.log` | 否 |
| `REQUEST_LOG_QUEUE_SIZE` | 请求日志队列条数上限，正整数 | `10000` | 否 |
| `REQUEST_LOG_MAX_BYTES` | 排队及进行中日志的载荷字节预算，不是进程总内存上限 | `67108864` | 否 |
| `CHARGE_REDIS_ADDR` | 独立日志 Redis 地址，不回退到 REDIS_ADDR | (空) | redis 模式必填 |
| `CHARGE_REDIS_PASSWORD` | 独立日志 Redis 密码 | (空) | 否 |
| `CHARGE_REDIS_DB` | 独立日志 Redis DB，非负整数 | `0` | 否 |
| `CHARGE_REDIS_POOL_SIZE` | 独立日志 Redis 池大小，正整数 | `10` | 否 |
| `CHARGE_REDIS_TIMEOUT` | 单次日志 Redis 写入超时，正 Go duration | `1s` | 否 |

## 产品变量映射

开启缓存时，`/api` 按 `pcode` 读内存，未命中执行 `GET gin-finapi:product-map:v1:<pcode>`；关闭时每次 GET。value 是字段名到 CSV 索引的 JSON。
Redis 客户端懒连接，`/health` 不检查配置源或Mongo；启用MySQL后可在Redis故障时回源。
Redis-only 模式仅在独立测试环境人工确认后准备如下映射；启用MySQL同步则由MySQL维护同名映射：

```text
SET gin-finapi:product-map:v1:10001 '{"var100001":0,"var100002":1,"var100003":2,"var100004":3,"var100005":4,"var100006":5}' NX
```

也可在确认目标 Redis 后执行 `go run cmd/init_redis.go` 初始化产品 10001；使用 SET NX，
不覆盖已有映射。指定其他产品及离线测试见 [初始化工具说明](../../cmd/README.md)。

旧 Hash 不再自动读取。切换前需对每个有效产品显式运行 `go run cmd/init_redis.go -pcode 10001 -migrate-map`，
验证目标 KV；迁移保留旧 Hash 和已有目标，支持旧版本回滚。应用不会自动执行迁移。

映射必须为非空 JSON 对象，字段名非空、索引为非负整数；重复字段、null、小数和溢出索引拒绝。
最终映射缺失或无效返回 HTTP 200/status 6001/产品异常；最终源不可用返回 HTTP 503/status 9002。
不使用硬编码回退。缓存容量默认值仅为初始保护值，不是生产容量结论。
Mongo t/v、gzip/CSV 格式不变，CSV 长度仍必须等于最大映射索引加一。

## Redis API Key 凭据

凭据存储于 `gin-finapi:auth:apikey:v1:<sha256(apikey)>`（摘要为原始 Key 的小写十六进制 SHA-256）。
开启缓存时优先读内存，未命中 GET Redis；关闭缓存时每次 GET。
String Value 严格使用以下 JSON，示例仅为占位：

```json
{"version":1,"secret":"<injected-secret>","status":1,"expires_at":0}
```

status 为 0/1；expires_at 为 Unix 秒，0 永不过期，当前时间达到到期值即拒绝。
secret 为 1–256 字节，记录最大 4 KiB；拒绝缺失、重复、未知字段和错误类型。
未知、禁用、到期 Key 返回 HTTP 400/status 2001；最终源故障或损坏凭据返回 503/9002。
服务不回退环境变量凭据。Redis-only 部署使用受控工具 SET NX 导入凭据和授权；
启用MySQL时先核对同步目标，启动后会覆盖同名配置。配置存储需具备持久化、备份和防淘汰措施。
初始化样例仅供显式授权的测试环境，生产客户/Key/授权由受控流程维护。
