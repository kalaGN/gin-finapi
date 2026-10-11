# 数据初始化工具

## Redis 产品变量映射

`init_redis.go` 将六变量 JSON 写入 Redis String `gin-finapi:product-map:v1:<pcode>`，产品默认为 `10001`。
读取根目录 `.env` 或环境中的 `REDIS_ADDR`、`REDIS_PASSWORD`、`REDIS_DB`、
`REDIS_POOL_SIZE`、`REDIS_TIMEOUT`，默认值与应用一致。无需配置 API Key 或 Mongo。
配置加载复用应用入口，因此已设置的限流及缓存参数也必须合法。

仅在确认目标 Redis 和 DB 后，从仓库根目录运行：

```bash
go run cmd/init_redis.go
go run cmd/init_redis.go -pcode 10002
```

使用 `SET NX`，已存在的产品键不会覆盖（包括已有无效 JSON）；不删键、不清库、不操作 Mongo。
执行失败返回非零状态，连接始终尝试关闭。不在应用启动时自动运行。
脚本使用独立 build tag 避免同目录双 main；默认 `go test ./...` 不包含它，另执行离线测试：

```bash
go test -race cmd/init_redis.go cmd/init_redis_test.go
go vet cmd/init_redis.go
```

CI 现已显式执行上述独立测试、vet 和单文件编译，不执行程序、不连接或写入 Redis。
初始化客户端与运行服务共用客户端选项构造器，独立持有自己的池；不会复用业务运行中的池。

## API Key 与产品 String KV 样例

显式添加 `-auth-samples` 才写入认证样例，默认仍只写产品映射 KV。
确认隔离 Redis 目标后，设置 `INIT_API_KEY`、`INIT_API_SECRET`，再运行：

```bash
# 以下仅为本地测试占位凭据，不用于生产。
export INIT_API_KEY='example-test-key'
export INIT_API_SECRET='example-test-secret'
go run cmd/init_redis.go -pcode 10001 -auth-samples
```

新增两个 String Key：

- `gin-finapi:auth:apikey:v1:<sha256(apikey)>`：
  `{"version":1,"secret":"<INIT_API_SECRET>","status":1,"expires_at":0}`。
- `gin-finapi:auth:apikey-product:v1:<sha256(apikey)>:10001`：
  `{"version":1,"status":1,"expires_at":0}`。

两条记录使用 SET NX，不覆盖已有记录或其 TTL，不输出 Key、摘要或 secret。
样例启用且永不过期，无 Redis TTL；仅适用于明确指定的测试环境。
三个写操作（映射、凭据、授权）不是事务，失败可能已部分写入；可重试补齐，
但“保留已有值”不代表已有值正确或匹配传入 secret。
当前服务在验签后检查产品授权；修改后等待下一轮缓存刷新或重启进程，业务到期逐次检查。

## 旧产品 Hash 显式迁移

新服务只读 String KV，不隐式回退旧 `mongo_map`。发布前核对所有有效 pcode 和目标 Redis/DB，
获得授权后，对每个产品单独执行（下列命令会写数据，本轮未执行）：

```bash
go run cmd/init_redis.go -pcode 10001 -migrate-map
```

工具 HGET 旧字段，复用线上 JSON 校验后 SET NX 新键；源缺失/损坏或写入失败返回非零。
不修改 JSON、不删除旧 Hash，不覆盖已有目标或其 TTL。目标已存在只表示“保留”，不保证值正确。
核对新旧值、类型及 TTL，离线/隔离环境验证查询后再切换；源正在变更时先协调暂停配置更新，
迁移并非跨键事务。旧 Hash 保留供回滚；切换后的 KV 更新需另外同步旧 Hash 才能无损回滚。

## MongoDB 初始化工具

## 功能说明

自动创建 MongoDB 测试数据：
- 创建 16 个集合：`data_20251101_0` 到 `data_20251101_f`
- 每个集合插入 1000 条模拟数据
- 数据结构与项目中 `mongo.go` 保持一致

## 使用方法

### 1. 配置环境变量

确保 `.env` 文件中包含以下配置：

```env
MONGODB_DSN=mongodb://localhost:27017
MONGODB_DATABASE=test_db
```

### 2. 执行初始化

**Linux/macOS:**
```bash
./cmd/init_mongo.sh
```

**Windows:**
```cmd
cmd\init_mongo.bat
```

**直接运行 Go 程序:**
```bash
go run cmd/init_mongo.go
```

## 数据结构

每条文档包含以下字段：

```json
{
  "t": "随机20位字母数字ID",
  "v": "gzip压缩后的数据（6个逗号分隔的随机整数0-1000）"
}
```

解压后的 `v` 字段数据格式：
```
"123,456,789,234,567,890"
```

对应的数据映射（由 Redis 产品 String KV 配置，`init_redis.go` 可初始化）：
- var100001: 第1个值 (索引0)
- var100002: 第2个值 (索引1)
- var100003: 第3个值 (索引2)
- var100004: 第4个值 (索引3)
- var100005: 第5个值 (索引4)
- var100006: 第6个值 (索引5)

## 注意事项

⚠️ **警告**: 执行脚本会先删除已存在的同名集合！

- 确保 MongoDB 服务已启动
- 确保有足够的数据库权限
- 建议在测试环境执行
- 16个集合 × 1000条数据 = 共16,000条文档

## 示例输出

```
Starting MongoDB initialization...
==================================
Connected to MongoDB successfully!
Initializing collection: data_20251101_0
Successfully inserted 1000 documents into data_20251101_0
Initializing collection: data_20251101_1
Successfully inserted 1000 documents into data_20251101_1
...
MongoDB initialization completed!
==================================
MongoDB initialization completed successfully!
Created 16 collections (data_20251101_0 to data_20251101_f)
Each collection contains 1000 documents
```
