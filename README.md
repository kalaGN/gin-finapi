# Gin FinAPI

基于 Gin Web 框架构建的 Go 语言 API 服务项目。

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
│   ├── middleware/          # 中间件
│   │   ├── loan_auth.go     # JSON API Key + 签名鉴权
│   │   ├── cors.go          # CORS
│   │   ├── logger.go        # 日志
│   │   ├── ratelimit.go     # 限流

│   ├── models/              # 数据模型
│   └── repositories/        # 数据仓库
├── bootstrap/               # 启动引导
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
- ✅ **MongoDB 连接池** - 单例模式，最大 100 连接
- ✅ **Redis 连接池** - 复用产品映射客户端；限流仍在进程内，不使用 Redis
- ✅ **优雅关闭** - 资源自动清理

### 配置管理
- ✅ **环境变量驱动** - 符合 12-Factor App 原则
- ✅ **默认值支持** - 启动校验 Mongo、签名密钥、API Key
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
curl -X POST http://localhost:8082/loan \
  -H "Content-Type: application/json" \
  -d '{"phone":"r707qyr0k2xmucjp7lz0","pcode":10001,"apikey":"your_api_key","timestamp":1730000000000,"sign":"UPPERCASE_MD5_32_CHARS"}'
```

## API 接口

### 健康检查

```
GET /health
```

返回：`ok`

### 数据接口

```
POST /loan
Content-Type: application/json
```

**请求体：**

```json
{
  "phone": "r707qyr0k2xmucjp7lz0",
  "pcode": 10001,
  "apikey": "your_api_key",
  "timestamp": 1730000000000,
  "sign": "UPPERCASE_MD5_32_CHARS"
}
```

phone 是非空字符串：完整 32 位十六进制 MD5 截前 20 位查 Mongo t，保留大小写；
既有 20 位查询标识继续原样查询。64 位十六进制 SHA-256 的转换服务尚未接入（TODO），
当前返回 HTTP 503 + 9002，不直接查库或对摘要再哈希。其他历史非空值沿用原值兼容。
签名按原规则对 phone、pcode、apikey、timestamp 排序拼接，加该 API Key 对应的 secret 后做大写 MD5。
请求 apikey 必须对应 Redis 中启用且未到期的凭据；timestamp 为过去五分钟内的 Unix 毫秒。
上面的 timestamp 和 sign 只是结构示意，联调时需要实时生成。
[设计与请求示例](doc/specs/20261011-请求加密鉴权-设计.md)。

请求必须使用 application/json，Body 最大 64 KiB。无效 API Key 返回 400/2001；
phone 类型错误返回 400/1002、缺失或空字符串返回 400/1001。其他错误映射保持原约定。
URL 参数和 nonce 不参与当前 JSON 验签；PHONE_AES_KEY 不再读取或必填。

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
| `REDIS_TIMEOUT` | 产品映射读取超时，正 Go duration | `1s` | 否 |

### 产品变量映射

`/loan` 每次请求按 `pcode` 执行 `HGET mongo_map <pcode>`，value 是字段名到 CSV 索引的 JSON。
Redis 已是业务必需依赖；客户端懒连接，`/health` 不检查 Redis/Mongo。
仅在独立测试环境人工确认后准备如下映射，应用不自动写入或覆盖配置：

```text
HSET mongo_map 10001 '{"var100001":0,"var100002":1,"var100003":2,"var100004":3,"var100005":4,"var100006":5}'
```

也可在确认目标 Redis 后执行 `go run cmd/init_redis.go` 初始化产品 10001；使用 HSETNX，
不覆盖已有映射。指定其他产品及离线测试见 [初始化工具说明](cmd/README.md)。

映射必须为非空 JSON 对象，字段名非空、索引为非负整数；重复字段、null、小数和溢出索引拒绝。
映射缺失或无效返回 HTTP 200/status 6001/产品异常；Redis 读失败或超时返回 HTTP 503/status 9002。
不使用硬编码回退或本地缓存。Mongo t/v、gzip/CSV 格式不变，CSV 长度仍必须等于最大映射索引加一。
生产客户端随正常关闭释放；现有启动 goroutine Fatal 的生命周期风险仍待独立方案处理。

## 开发

限流配置放在 `.env` 或环境变量中，修改后重启生效。非法次数或窗口会拒绝启动。
限流仍为单进程、按 IP，作用于所有路由；多实例间不共享计数。

### 添加新接口

1. 在 `app/Http/controllers/` 创建控制器
2. 在 main 装配依赖并向 `routes/api.go` 注入 Handler
3. 在业务路由注册认证中间件；`/loan` 必须挂载 LoanAuth

### 添加中间件

在 `bootstrap/route.go` 中注册全局中间件：

```go
router.Use(middleware.YourMiddleware())
```

或在特定路由组使用：

```go
api := router.Group("/api")
api.Use(middleware.LoanAuth(config.Config.APIKey, config.Config.SecretKey))
```

LoanAuth 专用于本项目 Loan JSON 契约；旧 AuthRequired/APIKeyAuth 仍是未启用占位实现。

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

- **开发环境**：文本格式，Debug 级别
- **生产环境**：JSON 格式，Info 级别

示例日志：

```json
{
  "level": "info",
  "method": "GET",
  "path": "/health",
  "status": 200,
  "duration": 0,
  "time": "2025-11-14 23:35:23",
  "msg": "Request completed"
}
```

## 许可证

MIT License

### Redis API Key 凭据

`POST /loan` 每次通过 GET 读取
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
本轮未提供或执行写入工具。API Key–产品授权结构已设计，尚未启用校验。
