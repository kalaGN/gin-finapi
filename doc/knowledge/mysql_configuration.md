# MySQL 配置持久化、回源与同步

核实日期：2026-10-15。来源：`pkg/mysql/`、`bootstrap/configuration.go`、`app/configdata/`、
`pkg/redis/config_sync.go` 及实际本地集成测试；设计见 [Spec](../specs/20261015-MySQL配置回源同步-设计.md)。

## 数据与职责

- Schema：[`schema.sql`](../../pkg/mysql/schema.sql)，无迁移框架；四表均 InnoDB、utf8mb4。
- `api_customers`：客户启停；`api_keys`：客户下多个 Key、独立 secret/状态/到期。
- `api_products`：pcode 与变量下标 JSON；`api_key_products`：每 Key 的独立产品授权。
- 主键/唯一键/外键 RESTRICT 与状态 CHECK 以 SQL 为准；不存在余额、套餐或扣费表。
- Key 仅存 SHA-256 小写摘要；secret 存 AES-256-GCM 二进制密文（nonce+密文+tag），
  AAD 为密钥ID+冒号+Key摘要。密钥不存数据库，不能从摘要恢复原 Key。
- created_at/updated_at 为 UTC 会话的 DATETIME(3)，expires_at 是 Unix 秒，0永久。
  无软删除或自动归档；保留策略待确认。结构变更先建 Spec 并审查迁移/回滚。
- 客户禁用折算 Key 状态；授权状态还折算 Key/客户状态，到期取 Key 与授权的最早非零时间。

## 读取与一致性

内存 → Redis → 可选 MySQL。仅 MYSQL_ENABLED=true 装配失败回源；无效连接/密文不返回原始错误。
管理清理接口 DELETE /config/cache 只清L1，不触发MySQL→Redis同步；修改MySQL后需先确认同步成功，
否则新请求仍可读取有效但较旧的Redis配置。接口默认关闭，独立Token及调用方式见 [接口说明](api_reference.md#清除本进程配置缓存)。
Redis 缺失、损坏或故障可回源，有效禁用/到期命中必须拒绝，不得被下层覆盖。
请求路径不写 Redis；内存开关只控制 L1，不关闭 MySQL 回源；不检查内存年龄。
MySQL 明确缺失分别返回原认证/授权/产品异常，故障返回服务异常，不将故障当作空数据。
MySQL 无效映射仍是产品异常；无效凭据/授权是服务异常。

同步先获得有界 Redis 租约，再用 MySQL 只读可重复读事务读取完整快照，解密/验证后发布。
Redis String Key/JSON 合同不变；禁用/到期也发布。同步独立池使用 REDIS_*，不使用 CHARGE_REDIS_*。
每轮成功刷新本进程已缓存项，其他进程按其刷新周期读取；非跨 SQL/Redis/L1 的原子一致性。
数据库删除项仅从同步清单中删除；如果还有未托管的 Redis 同名旧实体，缺失不会自动撤销它。
撤权优先显式禁用，并确认各进程刷新；故障时旧内存可能继续服务，不提供立即撤权保证。

## 同步运行与风险

- MYSQL_ENABLED 默认 false；启用后 MYSQL_SYNC_ENABLED 默认 true，立即异步一次，随后默认5m。
- MYSQL_SYNC_TIMEOUT 默认30s，租约为该预算+1s；同实例/多实例竞争跳过，令牌不符拒绝发布。
- `gin-finapi:mysql-sync:v1:managed` 为 Set 清单，`:lock` 为租约，不允许其他用途复用。
- 仅允许凭据/授权/映射三种精确键进入清单；污染清单导致同步失败，不删除未知键。
- Lua 提供执行隔离，不提供运行时错误回滚；OOM可能部分写，断网可能结果未知。可幂等重跑。
  需监测 Redis 内存和同步错误；大配置快照的阻塞时长、内存占用与生产规模待建立基线。
- secret 在 Redis/内存仍为明文，必须最小权限、受控网络；本地明确关闭TLS不代表生产可以关闭。
- 加密密钥须独立备份和限制访问，当前单KeyID；轮换须先设计密文迁移，不能直接替换环境密钥。
- 关闭先停同步/缓存再关闭 MySQL；不自动建表、不自动写样例、不增加热重载或消费任务。

## 运维信号

问题：是否在回源、同步是否成功、是否被锁跳过？对应结构化 event：
`config_source_fallback`（data_type/reason）、`mysql_redis_sync`（数量/清理数/耗时）、
`config_refresh_after_sync_failed`（失败数）。缺失回源 DEBUG、故障回源 WARN、同步失败 ERROR。
不输出 Key、摘要、secret、密码、DSN 或原始驱动错误；尚无新增指标/Tracing/飞书报警。
失败时先检查开关、连接/TLS/超时、密钥ID及密文，再检查租约/清单类型与 Redis 内存；禁止清库排障。
`/health` 仍是活性检查，不证明 MySQL/Redis 就绪。

## 实际验证与边界

本地 MySQL9.6.0 的 fin_api 已建四表并写合成样例：1客户、2Key、2产品、3授权。
第二次初始化新增计数全部为0，未覆盖已有值。测试通过真实 SQL 解密及读取。
临时 Redis7.4.2 验证缓存开关、缺失/损坏/断开回源、双源故障、禁用不可绕过、锁竞争、
非法快照/清单拒绝、托管删除、非托管键及 request_log 保留、重复同步、首次同步及L1刷新。
手动 dry-run 与两次 apply 均成功，临时 Redis 清单为7键。

上述验证只读本地 MySQL、写临时 Redis；未写现有业务Redis，未运行Mongo初始化，未重启服务。
未验证远端TLS/生产故障恢复/容量，也未验证本次 MySQL 配置下的真实 Mongo 全链路。
操作与离线/真实测试命令见 [`cmd/README.md`](../../cmd/README.md#mysql-配置初始化与同步)。

回滚：关闭 MYSQL_ENABLED 并按授权重启恢复Redis-only，保留四表；已发布配置需单独授权恢复。
