# Gin FinAPI

基于 Gin 的 HTTP 查询服务：Redis 提供 API Key 凭据、产品授权与变量映射，MongoDB 保存 gzip/CSV 数据；可选 MySQL 持久化配置、回源与同步。
当前接口为 `GET /health` 和 `POST /api`；开发规则与详细设计见 [文档索引](doc/README.md)。

## 当前实现（2026-10-15）

- 增加 MySQL 客户/Key/产品/授权四表，一个客户可有多个 Key；启用后 Redis 缺失或故障回源 MySQL，默认每5分钟同步到 Redis。见 [操作说明](cmd/README.md#mysql-配置初始化与同步)。
- `/api` 经中间件在认证前生成 SID，Handler 复用它并调用 `app/services/phone_lookup.go` 转换查询标识；查询规则不变。
- 限流器通过 `Start/Close` 管理清理任务，由 bootstrap 启动并在 HTTP 排空后关闭。
- 请求日志支持异步写文件或独立 Redis，记录完整请求体、实际响应、业务状态、success/charge 和毫秒时间。
- 限流采用每 IP 独立固定窗口；业务 Panic 先恢复再记录完成日志，已写响应不被覆盖。见 [修复设计](doc/specs/20261015-限流窗口与异常日志-设计.md)。
- 本轮未更新版本 Tag；具体变更及验证见 [生命周期与查询职责设计](doc/specs/20261014-限流生命周期与查询职责-设计.md)。

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
- **Redis** - String KV 存储凭据、产品授权和 `gin-finapi:product-map:v1:<pcode>` 映射
- **MySQL（可选）** - 配置持久化，`database/sql` + `go-sql-driver/mysql v1.10.1`；本地验证版本9.6.0
- **Logrus** - 日志系统
- **godotenv** - 环境变量管理

## 项目结构

```
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
│   └── utils/                # 签名、时间戳与 SID
├── routes/                   # GET /health、POST /api 路由定义
├── cmd/                      # 显式授权后使用的数据初始化工具
├── tests/                    # Apifox 用例、隔离集成测试与历史压测脚本
├── doc/                      # Rules、Specs、Knowledge
├── .github/workflows/ci.yml  # 格式、静态检查、竞态测试与构建门禁
├── .env.example              # 配置模板；本地 .env 不提交
└── main.go                   # 信号处理、调用 bootstrap.Run 与退出码
```

## 功能特性

### 中间件系统

- ✅ **日志中间件** - 结构化日志记录方法、路由、状态码、耗时及完整请求体与响应
- ✅ **异常恢复** - 使用 Gin 内置 Recovery
- ✅ **CORS 支持** - 跨域请求处理
- ✅ **限流保护** - 单进程按 IP 计数，次数/窗口可配置；默认 5000 次、1s 窗口，不代表吞吐基线
- ✅ **认证鉴权** - API Key + nonce/timestamp/sign 与产品授权；phone 按现有规则转为查询标识

### 数据库连接池

- ✅ **MongoDB 连接池** - 主链路由独立 Store 持有配置和懒连接，默认最大 100 连接
- ✅ **Redis 连接池** - 认证/产品映射各自复用一个池；REDIS_POOL_SIZE 分别作用于每池，不是应用总上限
- ✅ **优雅关闭** - 资源自动清理

### 配置管理

- ✅ **环境变量驱动** - 支持 `.env` 或等价环境变量，配置加载集中在启动入口
- ✅ **默认值支持** - 启动校验 Mongo/Redis 配置；API Key 与签名密钥从 Redis 凭据读取
- ✅ **环境模式** - `ENV` 控制 Gin 模式和应用日志格式；测试数据隔离须自行配置

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

启动前须由受控流程准备配置与 Mongo 数据；应用不自动建表或初始化样例。
`MYSQL_ENABLED=false` 为默认 Redis-only 模式；启用 MySQL 时须配置连接与独立加密密钥，
`MYSQL_SYNC_ENABLED=true` 默认启动后异步同步一次、此后每5分钟覆盖同名 Redis 配置。
首次使用先按 [MySQL 操作说明](cmd/README.md#mysql-配置初始化与同步) 检查目标、初始化及 dry-run。
工具说明见 [cmd/README.md](cmd/README.md)。`init_mongo.go` 会删除并重建 16 个集合，未经明确授权不得执行。

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

### 清除本进程配置缓存

配置独立 `CONFIG_CACHE_TOKEN` 并重启后启用 `DELETE /config/cache`；默认空值不注册路由（404）。
只从 `Authorization: Bearer <token>` 认证，不接受客户API Key、Query或Body中的Token；
未认证返回401，读取器已关闭或调用取消返回503。应使用HTTPS并限制管理路径的网络访问。

```bash
curl --request DELETE 'http://127.0.0.1:8082/config/cache' \
  --header 'Authorization: Bearer <独立管理Token>'
```

成功返回实际清理数量，缓存关闭或重复清理时可为0：

```json
{"cleared":{"credentials":1,"product_access":1,"product_maps":1}}
```

清理凭据、产品授权、产品映射的内存快照，不读写Redis/MySQL/Mongo；清理前在途读取不会回填旧值，
但已接受的请求仍可完成，后续请求可立即回源并重新缓存。多实例须分别调用，不能据此承诺立即撤权。
如果修改的是MySQL，须先完成MySQL→Redis同步，否则清理L1后仍可能读取Redis旧配置。
该管理接口无业务status字段、不计费；完成日志input/status为null、success/charge为0，另记录SID及清理数量审计。
管理接口不采集Body/Header/Query，错误响应为 `{"error":"未授权访问"}` 或 `{"error":"服务暂时不可用"}`。
详见 [清理设计](doc/specs/20261015-内存缓存清理-设计.md)。

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
所有 32 位字符串统一截前 20 位查 Mongo t，不要求十六进制、保留大小写；64 位十六进制 SHA-256
转换服务尚未接入（TODO），返回 HTTP 503/9002，不直接查库或对摘要再哈希。
64 位非十六进制字符串仍原样查询；不对标识重新哈希或自动补齐。
签名对 apikey、nonce、timestamp 按 key 排序拼接，加该 API Key 对应的 secret 后做大写 MD5：

```text
apikey=...&nonce=...&timestamp=...&key=<secret>
```

nonce 必须是非空 JSON 字符串，保留前导零，不转成数字；缺失/空串为 400/1001，类型错误为 400/1000。
不接受旧四字段或五字段签名，客户端须同步更新；本次未增加 nonce 去重，仍不能阻止窗口内的重复请求。
pcode、phone 不参与签名，但仍校验业务参数和产品授权；签名不保护这两个字段的完整性。
请求 apikey 必须对应有效配置源中启用且未到期的凭据，且对请求 pcode 有启用授权；timestamp 为过去五分钟内的 Unix 毫秒。
上面的 timestamp 和 sign 只是结构示意，联调时需要实时生成。
[设计与请求示例](doc/specs/20261011-请求加密鉴权-设计.md)。

请求必须使用 application/json，Body 最大 64 KiB。无效 API Key 返回 400/2001；
phone 类型错误返回 400/1002、缺失或空字符串返回 400/1001。其他错误映射保持原约定。
URL 参数不参与 JSON 验签，不替代 JSON nonce；PHONE_AES_KEY 不再读取或必填。

API Key 产品授权从 Redis String `gin-finapi:auth:apikey-product:v1:<sha256(apikey)>:<pcode>` 读取，
Value 为 `{"version":1,"status":1,"expires_at":0}`。凭据、授权和产品映射统一经 `app/configdata` 读取：
内存优先，未命中读 Redis 并回填，每 5 分钟后台刷新已缓存项；请求不判断缓存年龄。
`MYSQL_ENABLED=true` 时，Redis 缺失、损坏、连接故障或自身超时后读取 MySQL；有效禁用/到期记录不回退。
关闭 MySQL 时保持原行为。请求回源不写 Redis，成功可回填内存；调用方取消后不启动下一层读取。
`REDIS_CACHE_ENABLED=false` 关闭三类内存缓存，不回填、不合并读取、不启动 L1 刷新，但仍保留已启用的 MySQL 回源。
默认 `true`；修改后重启生效。两层存储均不可用才返回服务异常，没有旧内存快照兜底；认证与到期检查不变。
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
| `REQUEST_LOG_OUTPUT` | 请求日志后端，`file` 或 `redis` | `file` | 否 |
| `REQUEST_LOG_FILE` | 文件后端追加路径 | `request.log` | 否 |
| `REQUEST_LOG_QUEUE_SIZE` | 请求日志队列条数上限，正整数 | `10000` | 否 |
| `REQUEST_LOG_MAX_BYTES` | 排队及进行中日志的载荷字节预算，不是进程总内存上限 | `67108864` | 否 |
| `CHARGE_REDIS_ADDR` | 独立日志 Redis 地址，不回退到 REDIS_ADDR | (空) | redis 模式必填 |
| `CHARGE_REDIS_PASSWORD` | 独立日志 Redis 密码 | (空) | 否 |
| `CHARGE_REDIS_DB` | 独立日志 Redis DB，非负整数 | `0` | 否 |
| `CHARGE_REDIS_POOL_SIZE` | 独立日志 Redis 池大小，正整数 | `10` | 否 |
| `CHARGE_REDIS_TIMEOUT` | 单次日志 Redis 写入超时，正 Go duration | `1s` | 否 |

### 产品变量映射

开启缓存时，`/api` 按 `pcode` 读内存，未命中执行 `GET gin-finapi:product-map:v1:<pcode>`；关闭时每次 GET。value 是字段名到 CSV 索引的 JSON。
Redis 客户端懒连接，`/health` 不检查配置源或Mongo；启用MySQL后可在Redis故障时回源。
Redis-only 模式仅在独立测试环境人工确认后准备如下映射；启用MySQL同步则由MySQL维护同名映射：

```text
SET gin-finapi:product-map:v1:10001 '{"var100001":0,"var100002":1,"var100003":2,"var100004":3,"var100005":4,"var100006":5}' NX
```

也可在确认目标 Redis 后执行 `go run cmd/init_redis.go` 初始化产品 10001；使用 SET NX，
不覆盖已有映射。指定其他产品及离线测试见 [初始化工具说明](cmd/README.md)。

旧 Hash 不再自动读取。切换前需对每个有效产品显式运行 `go run cmd/init_redis.go -pcode 10001 -migrate-map`，
验证目标 KV；迁移保留旧 Hash 和已有目标，支持旧版本回滚。应用不会自动执行迁移。

映射必须为非空 JSON 对象，字段名非空、索引为非负整数；重复字段、null、小数和溢出索引拒绝。
最终映射缺失或无效返回 HTTP 200/status 6001/产品异常；最终源不可用返回 HTTP 503/status 9002。
不使用硬编码回退。缓存容量默认值仅为初始保护值，不是生产容量结论。
Mongo t/v、gzip/CSV 格式不变，CSV 长度仍必须等于最大映射索引加一。
`main` 只处理信号和退出码，`bootstrap.Run` 装配并管理运行；启动或服务失败也进入依赖清理。
正常关闭为 HTTP → 限流 → MySQL同步 → 配置缓存/回源 → MySQL → Mongo Store → Redis → 请求日志，
HTTP/依赖各有独立 5s 预算；HTTP Shutdown 失败时强制 Close。
服务 goroutine 退出等待另设 5s 保护，整个过程不承诺严格 10s 硬上限；需匹配部署终止宽限时间。
Mongo 仅通过 Store 实例查询和关闭，不读取全局配置；已移除旧 GetMongo/GetClient/包级 Close 入口。

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
git diff --check
```

默认测试不包含真实 Redis/Mongo 集成；隔离容器回归见 [集成测试说明](tests/integration/README.md)，
Apifox 导入见 [用例说明](tests/APIFOX.md)。数据测试只能指向专用、可丢弃环境。

## 部署边界

现有 GitHub Actions 只做检查和构建，不执行服务器部署；仓库没有 Dockerfile 或 Kubernetes 配置。
按目标 CPU 架构编译二进制，分别配置运行环境、Redis/Mongo 数据和进程管理；不要将密钥写入镜像或命令历史。
`/health` 仅返回存活响应，不代表数据库就绪；Tag、构建成功或本地测试通过也不代表已经部署。

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
  "duration_ms": 0,
  "success": 0,
  "charge": 0,
  "status": null,
  "input": "",
  "response_data": "ok",
  "time": 1791896584522
}
```

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

## 许可证

MIT License（当前 README 声明；仓库未提供独立 LICENSE 文件）。
