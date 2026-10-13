# Gin FinAPI

基于 Gin Web 框架构建的 Go 语言 API 服务项目。

## 版本 v1.0.1（2026-10-13）

本次按维护者要求采用补丁版本号；包含客户端不兼容调整，并非完全向后兼容的补丁。

- 业务接口统一为 `POST /api`，不再注册 `POST /loan`。
- phone 仅接受 32/64 字节；其他非空长度返回 HTTP 400/1002，旧短标识客户端须调整。
- 纳入 `.air.toml` 本地热重载配置，未将 Air 加入应用依赖。
- 当前签名仅包含 apikey、nonce、timestamp，附对应凭据 secret 后计算 MD5；旧签名客户端须同步。
- 飞书报警继续暂缓；SHA-256 转换服务仍未接入，64 位十六进制 phone 返回 503/9002。

应用版本由 Git Tag 管理，Go 与依赖版本仍由 `go.mod`、`go.sum` 管理；Tag 不代表已部署。

## 技术栈

- **Go 1.24.0 / toolchain 1.24.10**
- **Gin v1.11.0** - Web 框架
- **MongoDB** - 数据库（支持连接池）
- **Redis** - 按 pcode 读取 `mongo_map` 产品变量映射
- **Logrus** - 日志系统
- **godotenv** - 环境变量管理

## 项目结构

```
gin-finapi/
├── app/
│   ├── Http/controllers/    # 控制器层
│   │   └── loan/            # 贷款业务
│   ├── services/           # 产品查询编排与稳定错误语义
│   ├── auth/               # 不依赖存储的认证凭据与错误
│   ├── middleware/          # 中间件
│   │   ├── loan_auth.go     # JSON API Key + 签名鉴权
│   │   ├── cors.go          # CORS
│   │   ├── logger.go        # 日志
│   │   ├── ratelimit.go     # 限流

│   ├── models/              # 数据模型
│   └── repositories/        # 数据仓库
├── bootstrap/               # 依赖装配、HTTP 生命周期和关闭
├── pkg/                     # 工具包
│   ├── config/              # 配置管理
│   ├── env/                 # 环境变量
│   ├── logger/              # 日志工具
│   ├── mongo/               # MongoDB 操作
│   ├── redis/               # Redis 操作
│   └── rescode/             # 响应码定义
├── routes/                  # 路由定义
├── .env                     # 环境变量（不提交）
├── .env.example             # 环境变量模板
└── main.go                  # 程序入口
```

## 功能特性

### 中间件系统
- ✅ **日志中间件** - 结构化日志记录方法、路由、状态码与耗时，不记录 Body
- ✅ **异常恢复** - 使用 Gin 内置 Recovery
- ✅ **CORS 支持** - 跨域请求处理
- ✅ **限流保护** - 单进程按 IP 限流（5000 req/s）
- ✅ **认证鉴权** - Redis API Key + timestamp/sign；phone 原值查询

### 数据库连接池
- ✅ **MongoDB 连接池** - 主链路由独立 Store 持有配置和懒连接，默认最大 100 连接
- ✅ **Redis 连接池** - 认证/产品映射各自复用一个池；REDIS_POOL_SIZE 分别作用于每池，不是应用总上限
- ✅ **优雅关闭** - 资源自动清理

### 配置管理
- ✅ **环境变量驱动** - 符合 12-Factor App 原则
- ✅ **默认值支持** - 启动校验 Mongo/Redis 配置；API Key 与签名密钥从 Redis 凭据读取
- ✅ **多环境支持** - 开发/测试/生产环境隔离

## 快速开始

### 1. 安装依赖

```bash
go mod download
```

### 2. 配置环境变量

复制环境变量模板：

```bash
cp .env.example .env
```

编辑 `.env` 文件，配置你的环境：

```env
# 服务器配置
SERVER_PORT=8082
ENV=development
# 凭据通过受控维护写入 Redis，服务不读取 API_KEY/SECRET_KEY
RATE_LIMIT_REQUESTS=5000
RATE_LIMIT_WINDOW=1s

# MongoDB 配置
MONGODB_DSN=mongodb://localhost:27017
MONGODB_DATABASE=your_database
MONGODB_COLLECTION=data_20251101_0
MONGODB_TIMEOUT=5s
MONGODB_MAX_POOL=100
MONGODB_MIN_POOL=10

# Redis 配置
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=
REDIS_DB=0
REDIS_POOL_SIZE=10
REDIS_TIMEOUT=1s
```

### 3. 运行项目

**开发环境：**

```bash
go run main.go
```

**生产编译：**

```bash
# Linux
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o gin-finapi main.go

# macOS
go build -o gin-finapi main.go
```

### 4. 测试接口

```bash
# 健康检查
curl http://localhost:8082/health

# 数据接口
curl -X POST http://localhost:8082/api \
  -H "Content-Type: application/json" \
  -d '{"phone":"0123456789abcdef0123456789abcdef","pcode":10001,"apikey":"your_api_key","nonce":"3093","timestamp":1730000000000,"sign":"UPPERCASE_MD5_32_CHARS"}'
```

## API 接口

### 健康检查

```
GET /health
```

返回：`ok`

### 数据接口

```
POST /api
Content-Type: application/json
```

**请求体：**

```json
{
  "phone": "0123456789abcdef0123456789abcdef",
  "pcode": 10001,
  "apikey": "your_api_key",
  "nonce": "3093",
  "timestamp": 1730000000000,
  "sign": "UPPERCASE_MD5_32_CHARS"
}
```

phone 必须是长度为 32 或 64 字节的非空字符串，其他非空长度返回 HTTP 400/1002；旧 20 位标识不再接受。
完整 32 位十六进制 MD5 截前 20 位查 Mongo t，保留大小写；64 位十六进制 SHA-256
转换服务尚未接入（TODO），返回 HTTP 503/9002，不直接查库或对摘要再哈希。
本次仅限制长度，不要求十六进制字符；32/64 位非十六进制字符串仍原样查询。
签名对 apikey、nonce、timestamp 按 key 排序拼接，加该 API Key 对应的 secret 后做大写 MD5：

```text
apikey=...&nonce=...&timestamp=...&key=<secret>
```

nonce 必须是非空 JSON 字符串，保留前导零，不转成数字；缺失/空串为 400/1001，类型错误为 400/1000。
不接受旧四字段或五字段签名，客户端须同步更新；本次未增加 nonce 去重，仍不能阻止窗口内的重复请求。
pcode、phone 不参与签名，但仍校验业务参数和产品授权；签名不保护这两个字段的完整性。
请求 apikey 必须对应 Redis 中启用且未到期的凭据，且对请求 pcode 有启用授权；timestamp 为过去五分钟内的 Unix 毫秒。
上面的 timestamp 和 sign 只是结构示意，联调时需要实时生成。
[设计与请求示例](doc/specs/20261011-请求加密鉴权-设计.md)。

请求必须使用 application/json，Body 最大 64 KiB。无效 API Key 返回 400/2001；
phone 类型错误返回 400/1002、缺失或空字符串返回 400/1001。其他错误映射保持原约定。
URL 参数不参与 JSON 验签，不替代 JSON nonce；PHONE_AES_KEY 不再读取或必填。

API Key 产品授权从 Redis String `gin-finapi:auth:apikey-product:v1:<sha256(apikey)>:<pcode>` 读取，
Value 为 `{"version":1,"status":1,"expires_at":0}`。凭据、授权和产品映射统一经 `app/configdata` 读取：
内存优先，未命中读 Redis 并回填，每 5 分钟后台刷新已缓存项；请求不判断缓存年龄。
`REDIS_CACHE_ENABLED=false` 关闭三类内存缓存，每次请求直接读 Redis，不回填、不合并回源、不启动刷新任务。
默认 `true`；修改后重启生效。关闭时 Redis 故障直接返回服务异常，没有旧快照兜底；认证与到期检查不变。
授权缺失、禁用或到期返回 HTTP 403/2004；回源故障或损坏授权返回 HTTP 503/9002。
状态与业务到期时间每次请求检查。后台临时故障保留旧快照；明确删除或损坏清除旧值，禁用替换为拒绝状态。
撤销/轮换通常在下一轮同步完成后生效；持续故障时传播延迟无硬上限，不适合立即撤销。

**响应：**

```json
{
  "status": 0,
  "msg": "success",
  "sid": "xxxxxxxxxxxx",
  "data": {
    "var100001": 0,
    "var100002": 1,
    "var100003": 2,
    "var100004": 3,
    "var100005": 4,
    "var100006": 5
  }
}
```

## 环境变量说明

| 变量名 | 说明 | 默认值 | 必填 |
|--------|------|--------|------|
| `SERVER_PORT` | 服务端口 | `8082` | 否 |
| `ENV` | 运行环境 | `production` | 否 |
| `RATE_LIMIT_REQUESTS` | 每个 IP 在窗口内允许的次数，正整数 | `5000` | 否 |
| `RATE_LIMIT_WINDOW` | 限流窗口，正 Go duration，例如 `1s`、`500ms` | `1s` | 否 |
| `MONGODB_DSN` | MongoDB 连接字符串 | - | 是 |
| `MONGODB_DATABASE` | 数据库名称 | - | 是 |
| `MONGODB_COLLECTION` | 集合名称 | `data_20251101_0` | 否 |
| `MONGODB_TIMEOUT` | 查询超时 | `5s` | 否 |
| `MONGODB_MAX_POOL` | 最大连接数 | `100` | 否 |
| `MONGODB_MIN_POOL` | 最小连接数 | `10` | 否 |
| `REDIS_ADDR` | Redis 地址 | `localhost:6379` | 否 |
| `REDIS_PASSWORD` | Redis 密码 | (空) | 否 |
| `REDIS_DB` | Redis 数据库 | `0` | 否 |
| `REDIS_POOL_SIZE` | Redis 连接池大小 | `10` | 否 |
| `REDIS_TIMEOUT` | 配置合并回源及单次 Redis 读取超时，正 Go duration | `1s` | 否 |
| `REDIS_CACHE_REFRESH_INTERVAL` | 后台同步周期，正 Go duration | `5m` | 否 |
| `REDIS_CACHE_ENABLED` | 是否使用三类内存缓存，`true` / `false`，重启生效 | `true` | 否 |
| `REDIS_APIKEY_CACHE_MAX_ENTRIES` | 凭据 LRU 最大条目数，正整数 | `1000` | 否 |
| `REDIS_PRODUCT_AUTH_CACHE_MAX_ENTRIES` | 授权 LRU 最大条目数，正整数 | `10000` | 否 |
| `REDIS_PRODUCT_MAP_CACHE_MAX_ENTRIES` | 映射 LRU 最大条目数，正整数 | `1000` | 否 |
| `REDIS_CACHE_SYNC_CONCURRENCY` | 三类数据共用的回源并发上限，正整数 | `4` | 否 |

### 产品变量映射

开启缓存时，`/api` 按 `pcode` 读内存，未命中执行 `GET gin-finapi:product-map:v1:<pcode>`；关闭时每次 GET。value 是字段名到 CSV 索引的 JSON。
Redis 已是业务必需依赖；客户端懒连接，`/health` 不检查 Redis/Mongo。
仅在独立测试环境人工确认后准备如下映射，应用不自动写入或覆盖配置：

```text
SET gin-finapi:product-map:v1:10001 '{"var100001":0,"var100002":1,"var100003":2,"var100004":3,"var100005":4,"var100006":5}' NX
```

也可在确认目标 Redis 后执行 `go run cmd/init_redis.go` 初始化产品 10001；使用 SET NX，
不覆盖已有映射。指定其他产品及离线测试见 [初始化工具说明](cmd/README.md)。

旧 Hash 不再自动读取。切换前需对每个有效产品显式运行 `go run cmd/init_redis.go -pcode 10001 -migrate-map`，
验证目标 KV；迁移保留旧 Hash 和已有目标，支持旧版本回滚。应用不会自动执行迁移。

映射必须为非空 JSON 对象，字段名非空、索引为非负整数；重复字段、null、小数和溢出索引拒绝。
映射缺失或无效返回 HTTP 200/status 6001/产品异常；Redis 读失败或超时返回 HTTP 503/status 9002。
不使用硬编码回退。缓存容量默认值仅为初始保护值，不是生产容量结论；MySQL 尚未接入。
Mongo t/v、gzip/CSV 格式不变，CSV 长度仍必须等于最大映射索引加一。
`main` 只处理信号和退出码，`bootstrap.Run` 装配并管理运行；启动或服务失败也进入依赖清理。
正常关闭为 HTTP → 配置缓存 worker/回源 → Mongo Store → Redis，HTTP/依赖各有独立 5s 预算；HTTP Shutdown 失败时强制 Close。
服务 goroutine 退出等待另设 5s 保护，整个过程不承诺严格 10s 硬上限；需匹配部署终止宽限时间。
Mongo 仅通过 Store 实例查询和关闭，不读取全局配置；已移除旧 GetMongo/GetClient/包级 Close 入口。

## 开发

限流配置放在 `.env` 或环境变量中，修改后重启生效。非法次数或窗口会拒绝启动。
限流仍为单进程、按 IP，作用于所有路由；多实例间不共享计数。

### 添加新接口

1. 在 `app/Http/controllers/` 创建控制器
2. 在 main 装配依赖并向 `routes/api.go` 注入 Handler
3. 在业务路由注册认证中间件；`/api` 必须挂载 LoanAuth

### 添加中间件

在 `bootstrap/route.go` 中注册全局中间件：

```go
router.Use(middleware.YourMiddleware())
```

`/api` 的认证与产品授权由 `bootstrap/route.go` 注入
`middleware.LoanAuthWithProduct`；不要在路由层读取静态密钥或绕过产品授权。

## Docker 部署

仓库目前没有 Dockerfile，以下为历史部署示意，不能直接作为可执行部署流程。
上线前必须由受控维护方准备 Redis 凭据记录，不把密钥写入镜像或命令历史。

```bash
# 构建镜像
docker build -t gin-finapi:latest .

# 运行容器
docker run -d \
  -p 8082:8082 \
  -e SERVER_PORT=8082 \
  -e MONGODB_DSN=mongodb://host:27017 \
  -e MONGODB_DATABASE=mydb \
  gin-finapi:latest
```

## 日志

项目使用 Logrus 进行结构化日志记录：

请求完成日志增加整数 `success` 和 `charge`，按响应业务 `status` 计算：
`0 → success=1, charge=1`；`3001 → success=1, charge=0`；其他或无业务状态均为 `0/0`。
这不是 HTTP 状态码判断，也不执行实际扣费。仅记录状态元信息，不记录或缓存响应体。

请求完成日志由后台 worker 异步输出，可在 `.env` 中选择：

```env
REQUEST_LOG_OUTPUT=file
REQUEST_LOG_FILE=request.log
REQUEST_LOG_QUEUE_SIZE=10000
```

改为 `REQUEST_LOG_OUTPUT=redis` 时，需另设 `CHARGE_REDIS_ADDR`，并按需要配置
`CHARGE_REDIS_PASSWORD、CHARGE_REDIS_DB、CHARGE_REDIS_POOL_SIZE、CHARGE_REDIS_TIMEOUT`。
该连接与认证/产品映射 Redis 独立，不自动复用或回退。每条请求日志为 JSON，
使用 `RPUSH request_log` 追加到 List；后续消费者可从左侧消费，本项目暂不实现消费任务。
Redis 首次失败后重试两次，共最多三次尝试；仍失败则丢弃并写脱敏内部错误日志。
队列满不阻塞客户响应，丢弃并由后台汇总错误；启动/内部错误日志仍保留原输出方式。
关闭在现有预算内尽力排空。内存队列允许丢失，网络超时重试可能重复，不能作为唯一可靠计费账本。
不自动裁剪 List 或设置 TTL；需管理消费速度、Redis 容量和文件轮转。修改配置后重启生效。

- **开发环境**：文本格式，Debug 级别
- **生产环境**：JSON 格式，Info 级别

示例日志：

```json
{
  "level": "info",
  "method": "GET",
  "path": "/health",
  "http_status": 200,
  "duration_ms": 0,
  "success": 0,
  "charge": 0,
  "time": "2025-11-14 23:35:23",
  "msg": "Request completed"
}
```

## 许可证

MIT License

### Redis API Key 凭据

`POST /api` 每次通过 GET 读取
`gin-finapi:auth:apikey:v1:<sha256(apikey)>`（摘要为原始 Key 的小写十六进制 SHA-256）。
String Value 严格使用以下 JSON，示例仅为占位：

```json
{"version":1,"secret":"<injected-secret>","status":1,"expires_at":0}
```

status 为 0/1；expires_at 为 Unix 秒，0 永不过期，当前时间达到到期值即拒绝。
secret 为 1–256 字节，记录最大 4 KiB；拒绝缺失、重复、未知字段和错误类型。
未知、禁用、到期 Key 返回 HTTP 400/status 2001；Redis 故障或记录损坏返回 503/9002。
服务不自动创建记录，也不回退环境变量。请在部署切换前确认目标 Redis 并迁移旧凭据；
使用受控工具 SET NX 导入，避免覆盖；配置存储需具备持久化、备份和防淘汰措施。
API Key–产品授权需要在部署前写入 Redis；初始化样例工具可在显式参数授权下写入测试授权，生产授权由受控流程维护。
