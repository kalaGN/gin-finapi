# Knowledge 索引

Knowledge 只记录已确认的项目事实、当前实现和历史决策。强制约束写入
[`../rules/project_rules.md`](../rules/project_rules.md)，单次需求和设计遵循
[`../rules/spec_guidelines.md`](../rules/spec_guidelines.md)。

## 现有知识

| 文件 | 内容 | 使用场景 | 可信度与限制 |
| --- | --- | --- | --- |
| [`mysql_configuration.md`](mysql_configuration.md) | MySQL配置四表、加密、回源、定时/手动同步与真实验证 | 配置数据与同步运维 | 本地MySQL9.6.0和临时Redis7.4.2已验证；未写业务Redis、未验证生产容量 |
| [`performance_baseline_20261015.md`](performance_baseline_20261015.md) | 当前版本缓存开关、并发 1/32、异步文件日志的进程内实测 | 处理路径性能回归参考 | 合成内存源，无 Redis/Mongo/网络容量结论；真实依赖基线待建立 |
| [`project_analysis.md`](project_analysis.md) | 当前调用链、P1/P2 生命周期/实例配置/业务边界、Redis 映射与历史风险审计 | 规划、评审、整改排序 | 最新节覆盖历史描述；隔离 Redis/Mongo 链路已验证，生产断线恢复与容量待验证 |
| [`performance_test_report.md`](performance_test_report.md) | 2025-11-15 macOS / Go 1.23.3 的本地 ab 数据 | 性能历史对比 | 非生产容量承诺；部分原因和建议是未验证推测 |
| [`gin_migration_design.md`](gin_migration_design.md) | 从 Iris 迁移到 Gin 的历史设计 | 追溯迁移背景 | 当前迁移已完成；“当前 Iris/目标 Gin”等描述已过期 |
| [`../../README.md`](../../README.md) | 项目使用说明 | 本地启动和接口概览 | 2026-10-11 同步 phone 原值查询与 Redis API Key；Docker 仍是非可执行历史示意 |
| [`../../cmd/README.md`](../../cmd/README.md) | MySQL四表/样例/同步、Redis和Mongo初始化 | 经授权维护配置/测试数据 | Mongo会删集合；Redis init用SET NX保留已有键，MySQL同步覆盖同名配置 |
| [`../../tests/bench/README.md`](../../tests/bench/README.md) | 隔离 Go 基准与历史压测脚本说明 | 复跑进程内基线 | api_test.go 使用当前契约；旧 ab 脚本不符合当前契约，不用于验收 |

## 按任务加载

- 修改路由、Controller、配置或数据查询：读 `project_analysis.md`，再以相关源码为准。
- 修改配置持久化、回源或同步：再读 `mysql_configuration.md` 与对应 Spec，不使用真实凭据作为测试样本。
- 性能优化或容量讨论：先读当前合成基线，再按需读历史性能报告；必须区分替身与真实依赖，记录环境、数据和日志配置。
- 迁移历史或 Gin 选择背景：读迁移设计；不要复制其中已过期的 Iris 状态。
- 数据初始化：读 `cmd/README.md` 和 `cmd/init_mongo.go`，明确目标数据库并取得授权。

## 维护规则

- 每个结论附事实来源或验证日期；推测不得写成事实。
- 代码、配置或测试变化导致知识过期时，同一变更同步更新或显式标记过期。
- 性能报告保留测试环境、参数、原始指标和失败率；本地结果不得外推生产容量。
- 运维步骤必须写明前置条件、副作用、目标环境和回滚方式。
- 新增 Knowledge 后更新本索引。
- 聊天记录、生成代码注释和第三方示例不是长期事实来源。
