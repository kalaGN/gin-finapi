# 已确认业务场景与测试矩阵

核对日期：2026-10-16。范围：当前代码与已确认Spec中的规则，不包含尚未实现需求或所有可能环境故障。
最初补测仅改测试及文档；后续Mongo初始化边界修复同步补充DATA-03。测试源码与执行结果为证据。

## 离线场景

下表全部进入本轮默认全量竞态回归；CLI按独立命令另行执行。既有测试复用，不复制填数。

| ID | 场景与关键断言 | 测试证据 |
| --- | --- | --- |
| HTTP-01 | health公开、/api仅POST、旧/loan不再注册 | [routes/api_test.go](../../routes/api_test.go) |
| HTTP-02 | JSON/Content-Type/多JSON/64KiB、必填、null、类型、数值范围及非法长度拒绝 | [loan_auth_test.go](../../app/middleware/loan_auth_test.go)、[index_test.go](../../app/Http/controllers/loan/index_test.go) |
| HTTP-03 | timestamp过去窗口、未来/过期拒绝；签名排序、缺失、非法长度及错误secret | [signature_test.go](../../pkg/utils/signature_test.go)、[loan_auth_test.go](../../app/middleware/loan_auth_test.go) |
| HTTP-04 | nonce必填字符串、前导零、篡改拒绝；pcode/phone不签名但仍授权；无防重放去重 | TestLoanAuth_NonceSignatureContract、TestLoanAuthWithProduct_UnsignedProductStillRequiresAuthorization；[组合矩阵](../../bootstrap/config_cache_test.go)重复发送同一请求 |
| HTTP-05 | SID在认证前生成、失败短路、成功复用，失败响应SID仍为空 | [sid_test.go](../../bootstrap/sid_test.go)、[Handler](../../app/Http/controllers/loan/index_test.go) |
| HTTP-06 | phone32截20且保留大小写、64非hex原查、SHA256hex64当前503且不查询 | [phone_lookup_test.go](../../app/services/phone_lookup_test.go)、TestLoanBusinessScenarioMatrix |
| AUTH-01 | Redis有效禁用/到期不可被MySQL启用记录绕过；凭据400/2001、授权403/2004 | TestLoanBusinessScenarioMatrix、[fallback_test.go](../../app/configdata/fallback_test.go) |
| AUTH-02 | Redis缺失/损坏/故障回源；MySQL缺失、禁用、到期、损坏和故障正确映射，缓存开/关均验证 | TestLoanBusinessScenarioMatrix（35场景×2模式，共70组） |
| AUTH-03 | 客户禁用折算Key和授权状态，每Key独立授权，到期取Key/授权最早非零值 | TestReadCredentialAndCustomerState、TestReadProductAuthorizationBoundaries；[store_test.go](../../pkg/mysql/store_test.go) |
| QUERY-01 | 映射先于数据查询，缺失/坏映射6001、依赖故障9002；授权/映射失败不触及数据查询 | TestLoanBusinessScenarioMatrix、[product_query_test.go](../../app/services/product_query_test.go) |
| QUERY-02 | Mongo未查得3001、不可用9002、超时/取消9003、坏数据/未知错误9001；不泄露内部异常 | TestLoanBusinessScenarioMatrix、[diagnostics_test.go](../../app/Http/controllers/loan/diagnostics_test.go) |
| DATA-01 | BSON t/v与gzip/CSV合同、严格整数/字段数、索引映射和坏压缩拒绝 | [contract_test.go](../../pkg/mongo/contract_test.go)、[codec_test.go](../../pkg/mongo/codec_test.go)、[mongo_test.go](../../pkg/mongo/mongo_test.go) |
| DATA-02 | Mongo初始化失败不查询、默认集合/查询预算、Find失败/坏gzip传播 | TestStoreQueryFailureBoundaries；[store_test.go](../../pkg/mongo/store_test.go) |
| DATA-03 | 慢初始化不持锁、独立等待取消、同轮失败共享/后续重试、首请求取消隔离、总预算/上游截止、关闭取消/迟到候选清理/重复关闭 | [mongo_test.go](../../pkg/mongo/mongo_test.go)、[store_test.go](../../pkg/mongo/store_test.go)；[修复Spec](../specs/20261016-Mongo初始化边界-设计.md) |
| CACHE-01 | 无年龄TTL、逐请求状态/到期、LRU、缺失不缓存、刷新旋转/删除/损坏/故障保留 | [reader_test.go](../../app/configdata/reader_test.go) |
| CACHE-02 | 合并读取、等待者独立取消、并发门/排队预算、映射拷贝隔离、关闭取消回源 | [reader_test.go](../../app/configdata/reader_test.go)、[concurrent_test.go](../../app/configdata/concurrent_test.go) |
| CACHE-03 | 内存关闭逐次读源，不回填/合并；MySQL回源仍有效 | TestReaderDisabledReadsEverySourceAndNeverCaches、TestLoanBusinessScenarioMatrix |
| ADMIN-01 | 空Token404、独立Bearer、缺失/错误/重复认证401、只允许DELETE、无Query/Body兜底 | [config_cache_auth_test.go](../../app/middleware/config_cache_auth_test.go)、[api_test.go](../../routes/api_test.go)、[route_test.go](../../bootstrap/route_test.go) |
| ADMIN-02 | 清三类数量、重复/禁用为0、取消/关闭拒绝、旧flight晚到不回填/不删除新flight | [clear_test.go](../../app/configdata/clear_test.go)、[cache_test.go](../../app/Http/controllers/cache_test.go) |
| SQL-01 | 构造懒连接、池配置、TLS模式/系统CA/自定义CA与名称、缺失/坏CA拒绝 | TestMySQLConstructorTLSAndPoolConfiguration（只校验构造，不证明真实TLS握手） |
| SQL-02 | Ping成功/故障/超时/取消、SQL参数化、缺失与坏授权/溢出/错产品、错误脱敏 | TestMySQLPingPreservesSafeFailureClassification、TestMySQLProductAccessRejectsMissingCorruptAndMismatchedRows |
| SQL-03 | 密文往返/随机nonce、篡改/摘要与KeyID绑定、非法key/secret拒绝 | [secret_test.go](../../pkg/mysql/secret_test.go) |
| SQL-04 | 只读一致性事务；每类query/scan/重复/迭代失败、孤立引用、begin/commit失败不返回部分快照 | TestSnapshotRejectsEveryPartialFailure、TestSnapshotConsistencyAndPartialFailure |
| SQL-05 | 完整空快照与读取失败区分；禁用/到期记录仍可发布，摘要/版本/状态/引用/映射校验 | TestMySQLMissingMappingAndCompleteEmptySnapshot、[config_snapshot_test.go](../../app/services/config_snapshot_test.go) |
| INIT-01 | 四表幂等DDL失败停止；样例保留已有值，固定ID冲突拒绝，8个写入阶段失败回滚不提交 | [seed_test.go](../../pkg/mysql/seed_test.go)（SQL替身，不证明真实约束） |
| SYNC-01 | 锁竞争跳过、完整校验后发布、坏快照无写、错误发布响应/释放/关闭安全失败 | [config_sync_test.go](../../pkg/redis/config_sync_test.go)（不执行真实Lua） |
| SYNC-02 | 先租约后读取、失败不发布/不刷新、释放锁、首次/周期执行、取消/关闭等待 | [sync_test.go](../../app/configdata/sync_test.go) |
| BOOT-01 | MySQL/同步开关装配、错误配置与部分初始化失败、构造不联网 | TestConfigurationModesConstructAndCloseWithoutConnecting、TestNewRouterConstructorRejectsPartialInitialization |
| BOOT-02 | 同步成功/跳过/失败/正常取消日志，刷新失败保留快照，HTTP关闭前于依赖且超时强制关闭 | [configuration_test.go](../../bootstrap/configuration_test.go)、[lifecycle_test.go](../../bootstrap/lifecycle_test.go) |
| LOG-01 | 全量Body/实际响应/status/time、success/charge、无Header/Query，管理接口不记Body/Token/charge | [logger_test.go](../../app/middleware/logger_test.go)、[config_cache_auth_test.go](../../app/middleware/config_cache_auth_test.go) |
| LOG-02 | 异步文件/Redis、字节与条数预算、非阻塞丢弃、最多3次尝试、关闭取消、下条记录继续 | [request_log_test.go](../../pkg/logger/request_log_test.go) |
| LOG-03 | Panic恢复后只记录一次实际完成响应；外层Recovery仍保护日志自身 | [panic_log_test.go](../../bootstrap/panic_log_test.go) |
| LOG-04 | duration_ms为累计毫秒对象；鉴权先于业务、短路缺失后续节点、Panic/health/限流结束、文件与Redis保留对象；公共计时并发/快照隔离 | [timing_test.go](../../pkg/utils/timing_test.go)、[logger_test.go](../../app/middleware/logger_test.go)、[文件后端](../../bootstrap/request_log_test.go)、[Redis输出替身](../../pkg/logger/request_log_test.go)；[设计](../specs/20261016-请求流程计时-设计.md) |
| LOG-05 | Mongo客户端等待/查询/解压/CSV累计节点，初始化失败/超时/取消、未查得、查询故障、坏gzip/CSV不伪造后续节点 | TestStoreQueryTimingStages；[store_test.go](../../pkg/mongo/store_test.go) |
| LOG-06 | 顶层apikey取/api完整Body；成功/认证失败/限流保留请求值，缺失/坏类型/坏JSON/截断/非业务为空；不读取Header/Query或管理Token | TestLogger_RequestResultFlags、TestLoggerAPIKeyInputBoundaries；[管理隔离](../../app/middleware/config_cache_auth_test.go) |
| LOG-07 | pcode严格整数/空值及实例版本不可由客户端覆盖；来源真实冷读/内存/回源、并发等待者/取消/刷新隔离 | [来源测试](../../app/configdata/source_test.go)、[记录器](../../pkg/utils/config_sources_test.go)、[身份配置](../../pkg/config/log_identity_test.go)、TestLoggerProductAndIdentityBoundaries |
| LIMIT-01 | 独立IP固定窗口、不滑动、并发配额、worker显式Start/Close及有界关闭 | [ratelimit_test.go](../../app/middleware/ratelimit_test.go) |
| CLI-01 | init/sync拒绝坏env或关闭配置且不连接；dry-run不触碰Redis，失败/锁竞争/发布与释放错误 | [init_mysql_test.go](../../cmd/init_mysql_test.go)、[sync_mysql_redis_test.go](../../cmd/sync_mysql_redis_test.go) |
| CLI-02 | Redis SET NX保留已有键、显式样例、旧Hash迁移拒绝坏源并保留源和目标 | [init_redis_test.go](../../cmd/init_redis_test.go) |

## 本次指标

以下是业务场景补测提交2b8bb03的历史快照，不代表后续Mongo生命周期修复后的覆盖率。

同一口径：`go test ./... -coverpkg=./... -coverprofile=...`，合并跨包命中后使用go tool cover；
不是把每个包打印的百分比简单平均。CLI因build tag另测，不并入默认模块覆盖率。

| 范围 | 补充前 | 本次实测 |
| --- | --- | --- |
| 整体 | 80.7% | 88.6%（1693/1911语句） |
| app/services | 88.6% | 98.6% |
| MySQL | 70.3% | 96.8% |
| Redis | 80.7% | 94.5% |
| Bootstrap | 65.4% | 84.9% |
| MySQL初始化CLI | 19.2% | 26.9% |
| 手动同步CLI | 31.4% | 54.9% |

入口main/真实初始化成功路径未为追覆盖率而执行；不设置未经团队确认的覆盖率门槛。
不同调度下少量取消/定时分支的语句命中可能波动，不是业务场景验收的替代指标。

## 明确未在本轮执行的场景

| 范围 | 现有资产/后续验证 | 本轮状态 |
| --- | --- | --- |
| 真实Redis/Mongo HTTP查询、真实Lua、迁移 | [隔离集成说明](../../tests/integration/README.md)，需专用可丢弃实例 | 本轮未执行；不能由替身结果证明 |
| MySQL实际FK/CHECK/JSON、真SQL初始化/同步 | [MySQL工具说明](../../cmd/README.md)，须确认隔离目标与写入范围 | 本轮未执行，未读本地真实.env或业务库 |
| MySQL远端证书验证/握手、断线重连、数据库重启 | 专门隔离TLS/故障环境 | 未验证 |
| Redis运行时OOM/脚本部分写入、故障转移 | 故障注入与恢复演练；Lua不承诺运行时回滚 | 未验证 |
| 多实例撤权时延、生产P95/P99/容量、运维权限 | 部署环境测试与可比压测 | 未验证 |
| SHA256转换服务、报警、日志消费/实际计费 | 需求仍暂缓或未实现 | 非当前已实现业务，不伪造成功用例 |

## 新确认的生命周期边界

Reader与同步worker可重复关闭；`configurationRuntime.close`第二次调用会报告Redis ErrClosed，
不是完整幂等。新增测试保留这一错误语义；生产实现未改。调用层应单次关闭，
如需要全依赖幂等关闭，应先确认需求再最小修复，不能从单个组件幂等推断整体幂等。

## 维护和复跑

命令及本轮结果见 [回归Spec](../specs/20261016-业务场景回归-设计.md)。新增/更改业务先更新场景及断言，
有新协议或数据合同则先建Spec；新业务不能仅依赖旧覆盖率报告。生产故障补具体复现用例，不把聊天作为规则。
