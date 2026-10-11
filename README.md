# Gin FinAPI

基于 Gin Web 框架构建的 Go 语言 API 服务项目。

## 技术栈

- **Go 1.18+**
- **Gin v1.11.0** - Web 框架
- **MongoDB** - 数据库（支持连接池）
- **Redis** - 缓存
- **Logrus** - 日志系统
- **godotenv** - 环境变量管理

## 项目结构

```
gin-finapi/
├── app/
│   ├── Http/controllers/    # 控制器层
│   │   └── loan/            # 贷款业务
│   ├── middleware/          # 中间件
│   │   ├── auth.go          # 认证鉴权
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
- ✅ **日志中间件** - 结构化日志记录请求/响应
- ✅ **异常恢复** - 使用 Gin 内置 Recovery
- ✅ **CORS 支持** - 跨域请求处理
- ✅ **限流保护** - 防止接口滥用（100 req/min）
- ✅ **认证鉴权** - JWT/API Key 支持

### 数据库连接池
- ✅ **MongoDB 连接池** - 单例模式，最大 100 连接
- ✅ **Redis 连接池** - 支持普通和限流两个实例
- ✅ **优雅关闭** - 资源自动清理

### 配置管理
- ✅ **环境变量驱动** - 符合 12-Factor App 原则
- ✅ **默认值支持** - 无配置也能运行
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
SECRET_KEY=replace_with_a_strong_random_secret

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
  -d '{"phone":"test123","pcode":10001,"apikey":"your_api_key","timestamp":1730000000000,"sign":"UPPERCASE_MD5_32_CHARS"}'
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
  "phone": "string",
  "pcode": 10001,
  "apikey": "your_api_key",
  "timestamp": 1730000000000,
  "sign": "UPPERCASE_MD5_32_CHARS"
}
```

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
| `SECRET_KEY` | 请求签名密钥 | - | 是 |
| `MONGODB_DSN` | MongoDB 连接字符串 | - | 是 |
| `MONGODB_DATABASE` | 数据库名称 | - | 是 |
| `MONGODB_COLLECTION` | 集合名称 | `data_20251101_0` | 否 |
| `MONGODB_TIMEOUT` | 连接超时 | `5s` | 否 |
| `MONGODB_MAX_POOL` | 最大连接数 | `100` | 否 |
| `MONGODB_MIN_POOL` | 最小连接数 | `10` | 否 |
| `REDIS_ADDR` | Redis 地址 | `localhost:6379` | 否 |
| `REDIS_PASSWORD` | Redis 密码 | (空) | 否 |
| `REDIS_DB` | Redis 数据库 | `0` | 否 |
| `REDIS_POOL_SIZE` | Redis 连接池大小 | `10` | 否 |

## 开发

### 添加新接口

1. 在 `app/Http/controllers/` 创建控制器
2. 在 `routes/api.go` 注册路由
3. 可选：添加认证中间件

### 添加中间件

在 `bootstrap/route.go` 中注册全局中间件：

```go
router.Use(middleware.YourMiddleware())
```

或在特定路由组使用：

```go
api := app.Party("/api")
api.Use(middleware.AuthRequired())
```

## Docker 部署

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
