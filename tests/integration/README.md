# Redis / MongoDB 集成回归

从仓库根目录执行（需要项目 Go 工具链和可用的 Docker）：

```bash
bash tests/integration/run.sh
```

脚本创建 Redis 7.4.7 和 MongoDB 8.0.16 临时容器，使用随机本机端口，不挂载开发数据目录；
等待数据库就绪后执行竞态检查，退出时删除本次创建的容器。无需运行应用或数据初始化 CLI。
首次运行需要下载镜像，后续复用镜像。集成测试不在默认 `go test ./...` 中执行。

覆盖内容：

- 通过真实 `NewRouter` 装配，读取 Redis 凭据、产品授权与 String 映射，查询 Mongo gzip 数据，校验六变量响应。
- 缓存与直读模式下重复请求；修改授权后，缓存保留快照、直读立即拒绝。
- 授权缺失、禁用、到期为 403/2004；损坏记录和错误 Redis 类型为 503/9002。
- 授权失败时，即使 Mongo 不可用，也返回授权错误。
- 旧 Hash 迁移至 String，重复执行不覆盖目标，保留源数据，拒绝无效映射。

测试只使用合成凭据和标识；Mongo 数据库名称随机且测试后删除；Redis 只清理本次创建的测试键。
不调用 FLUSHDB/FLUSHALL，不调用会重建测试集合的 `init_mongo.go`。

如需手动执行，`FINAPI_TEST_REDIS_ADDR` 和 `FINAPI_TEST_MONGO_DSN` **必须指向专用、可丢弃的测试服务**。
不要指定应用或生产数据库。缺少变量时显式测试失败；已有映射或迁移源会阻止测试写入。

```bash
FINAPI_TEST_REDIS_ADDR=127.0.0.1:16379 \
FINAPI_TEST_MONGO_DSN=mongodb://127.0.0.1:17017 \
go test -race -count=1 -tags integration ./bootstrap -run '^TestLoanIntegration$' -v

FINAPI_TEST_REDIS_ADDR=127.0.0.1:16379 \
go test -race -count=1 cmd/init_redis.go cmd/init_redis_integration_test.go -v
```

nonce 缺失、类型错误、篡改和旧签名已由离线中间件用例覆盖；nonce 没有防重放去重。
并发读取/刷新、返回映射修改隔离、关闭后拒绝读取由 `app/configdata` 的竞态测试覆盖。
