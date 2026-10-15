# Gin FinAPI 文档与规则索引

本项目使用 `doc/` 作为文档根目录，并按职责分为 `rules/`、`specs/` 和 `knowledge/`。

## 文档地图

| 文档 | 类别 | 职责 | 适用任务 |
| --- | --- | --- | --- |
| [`specs/20261015-限流窗口与异常日志-设计.md`](specs/20261015-限流窗口与异常日志-设计.md) | Specs / 设计 | 独立 IP 固定窗口及业务 Panic 完成日志 | 限流边界与异常响应日志 |
| [`specs/20261014-认证契约合并-设计.md`](specs/20261014-认证契约合并-设计.md) | Specs / 设计 | 凭据/授权类型与认证错误合并至 services，保留契约 | 认证引用与包职责维护 |
| [`specs/20261014-限流生命周期与查询职责-设计.md`](specs/20261014-限流生命周期与查询职责-设计.md) | Specs / 设计 | 限流任务显式启动与关闭，phone 转换归 Service | 生命周期与查询职责维护 |
| [`specs/20261013-完整请求日志-设计.md`](specs/20261013-完整请求日志-设计.md) | Specs / 设计 | 完整 Body/响应、业务码、毫秒时间和队列字节预算 | 请求日志字段与敏感数据边界 |
| [`specs/20261013-异步请求日志-设计.md`](specs/20261013-异步请求日志-设计.md) | Specs / 设计 | 文件或独立 Redis 后端、异步队列及两次重试 | 请求日志配置、消费契约与关闭 |
| [`specs/20261013-请求结果日志-设计.md`](specs/20261013-请求结果日志-设计.md) | Specs / 设计 | 请求日志 success/charge 按业务码计算；不缓存 Body 为历史方案 | 请求日志与计费标记 |
| [`specs/20261013-产品查询迁移-设计.md`](specs/20261013-产品查询迁移-设计.md) | Specs / 设计 | 查询编排迁入 services，app 新增目录须事先确认 | 目录职责、查询引用与架构维护 |
| [`specs/20261013-phone长度校验-设计.md`](specs/20261013-phone长度校验-设计.md) | Specs / 设计 | phone 32/64 位长度约束与兼容边界 | 参数校验与客户端回归 |
| [`specs/20261012-nonce签名-设计.md`](specs/20261012-nonce签名-设计.md) | Specs / 设计 | nonce 纳入客户端签名、兼容与验证 | 签名、nonce 参数校验 |
| [`specs/20261011-Redis内存缓存-设计.md`](specs/20261011-Redis内存缓存-设计.md) | Specs / 设计 | 统一读取、五分钟刷新及产品 KV 迁移，已实施 | 凭据、授权、映射缓存与迁移 |
| [`specs/20261011-架构边界优化-设计.md`](specs/20261011-架构边界优化-设计.md) | Specs / 设计 | P1/P2 架构边界优化，已实施 | 生命周期、实例配置、业务与存储边界、CLI 门禁 |
| [`specs/20261011-APIKey Redis存储-设计.md`](specs/20261011-APIKey%20Redis存储-设计.md) | Specs / 设计 | Redis API Key 凭据、pcode 授权及授权缓存已实施 | 认证与产品授权 |
| [`specs/20261011-产品变量映射-设计.md`](specs/20261011-产品变量映射-设计.md) | Specs / 设计 | Redis 产品变量映射与异常分类，已实施 | 产品映射配置化 |
| [`specs/20261011-启动关闭重构-设计.md`](specs/20261011-启动关闭重构-设计.md) | Specs / 设计 | 启动、错误通道和资源关闭，已实施 | 生命周期重构 |
| [`specs/20261011-Mongo职责整理-设计.md`](specs/20261011-Mongo职责整理-设计.md) | Specs / 设计 | Mongo 同包职责整理方案及验证，已实施 | Mongo 内部重构 |
| [`../AGENTS.md`](../AGENTS.md) | 入口 | 项目定位、命令、红线和渐进加载入口 | 每次 AI Coding 任务 |
| [`rules/`](rules/README.md) | Rules 索引 | 长期强制规则和 Spec 管理规范 | 所有代码和文档变更 |
| [`rules/project_rules.md`](rules/project_rules.md) | Rules | 项目、架构、契约、编码、测试、安全、可靠性、性能、数据、Git 与 AI 规则 | 所有代码和文档变更 |
| [`rules/spec_guidelines.md`](rules/spec_guidelines.md) | Rules / Specs | 何时创建 Spec、命名、模板和生命周期 | 复杂需求、设计、迁移 |
| [`specs/`](specs/README.md) | Specs 索引 | 需求、设计和任务 Spec 的分层入口 | 复杂需求、设计、迁移 |
| [`specs/20260803-apikey账号鉴权-设计.md`](specs/20260803-apikey账号鉴权-设计.md) | Specs / 设计 | API Key 三层存储与每账号独立 secret 鉴权方案 | `/loan` API Key 鉴权实施前评审 |
| [`specs/20260803-apikey账号鉴权-任务.md`](specs/20260803-apikey账号鉴权-任务.md) | Specs / 任务 | API Key 账号鉴权的依赖顺序、开发切片和验证门禁 | 鉴权实施与分阶段评审 |
| [`specs/20260912-核心稳定性修复-需求.md`](specs/20260912-核心稳定性修复-需求.md) | Specs / 需求 | SID、Mongo 生命周期、错误治理、数据校验、参数、签名比较和测试修复范围 | 审计问题 3–10 验收与追溯 |
| [`specs/20260912-核心稳定性修复-设计.md`](specs/20260912-核心稳定性修复-设计.md) | Specs / 设计 | 核心稳定性修复的最小实现和兼容边界 | 审计问题 3–10 实施 |
| [`specs/20260912-核心稳定性修复-任务.md`](specs/20260912-核心稳定性修复-任务.md) | Specs / 任务 | 核心稳定性修复的测试优先任务与验证门禁 | 审计问题 3–10 实施跟踪 |
| [`specs/20261011-项目更名-需求.md`](specs/20261011-项目更名-需求.md) | Specs / 需求 | Airis 到 Gin FinAPI 的最小影响迁移范围与兼容边界 | 项目/module 更名 |
| [`specs/20261011-项目更名-任务.md`](specs/20261011-项目更名-任务.md) | Specs / 任务 | module、活跃文档和构建产物名称迁移步骤 | 项目/module 更名实施 |
| [`knowledge/`](knowledge/README.md) | Knowledge 索引 | 已确认知识、审计、性能数据和历史决策 | 排障、架构、性能、历史背景 |
| [`knowledge/project_analysis.md`](knowledge/project_analysis.md) | Knowledge / 审计 | 当前仓库事实、风险、证据和待确认项 | 规划整改、Code Review |
| [`knowledge/performance_test_report.md`](knowledge/performance_test_report.md) | Knowledge | 2025-11-15 本地 Apache Bench 历史结果 | 性能变更前的历史参考 |
| [`knowledge/gin_migration_design.md`](knowledge/gin_migration_design.md) | Knowledge / 历史设计 | Iris 迁移 Gin 的历史方案 | 追溯迁移决策；不能作为当前实现说明 |
| [`../README.md`](../README.md) | 使用说明 | 项目介绍、环境变量和启动示例 | 本地启动；内容需与代码交叉验证 |
| [`../cmd/README.md`](../cmd/README.md) | 操作说明 | MongoDB 测试数据及 Redis 产品映射初始化 | 仅明确授权的数据初始化任务 |
| [`../tests/bench/README.md`](../tests/bench/README.md) | 操作说明 | 历史压测脚本使用方法 | 非生产压测，需先校验请求契约 |

## 渐进加载规则

2026-10-15：[诊断日志与读取边界设计](specs/20261015-日志读取与性能基线-设计.md)、
[当前进程内性能基线](knowledge/performance_baseline_20261015.md)；真实 Redis/Mongo 容量尚未验证。

Redis/Mongo 集成回归：[独立容器测试说明](../tests/integration/README.md)。

接口回归资产：[Apifox 导入与执行说明](../tests/APIFOX.md)，包含 Postman v2.1 用例及无密钥环境模板。

1. 先读 `AGENTS.md` 和本索引。
2. 所有变更加载 `rules/project_rules.md` 中与任务有关的章节，不必一次加载全文。
3. 复杂任务再加载 `specs/` 中的对应 Spec；不存在 Spec 且满足创建条件时，先按
   `rules/spec_guidelines.md` 建立。
4. 只加载与任务有关的 Knowledge，并回到源代码、测试或配置验证可能过期的描述。
5. 修改文件前读取目标文件、相关测试和一个现有相似实现；失败后只补充相关错误输出。

## 冲突处理

优先级从高到低：

1. 用户当前明确要求。
2. 已确认且仍有效的需求 Spec。
3. 项目 Rules。
4. 项目 Knowledge。
5. 当前代码惯例。

安全限制、平台权限和法律合规约束不因上述排序失效。发现冲突时必须列出冲突位置、影响和
可选方案，等待确认或采用不会改变契约的最小安全动作；不得静默选择。代码与文档不一致时，
代码只能证明“当前行为”，不能自动证明该行为就是正确需求。

## 更新方式

- 长期强制约束更新 `rules/project_rules.md`，同时检查 `AGENTS.md` 是否需要调整索引或红线。
- 单次需求、设计和任务按 `rules/spec_guidelines.md` 管理，不把临时决策写成全局规则。
- 已确认实现、历史决策或运维事实写入 `knowledge/`，并更新 `knowledge/README.md`。
- 新增文档必须放入与职责对应的 `rules/`、`specs/` 或 `knowledge/`，并同步修改相应索引和相对链接。
- 每次更新检查相对链接、Markdown 格式、尾随空格和 `git diff --check`。

## 请求加密与鉴权（2026-10-11）

- [需求](specs/20261011-请求加密鉴权-需求.md)：已修正为 phone 原值查询，保留 LoanHandler 注入；静态 API Key 已由后续 Redis 方案替代。
- [设计](specs/20261011-请求加密鉴权-设计.md)：原查询标识兼容修正、职责和客户端示例。
- [任务](specs/20261011-请求加密鉴权-任务.md)：实施与验证记录。
