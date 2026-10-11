# Gin FinAPI 文档与规则索引

本项目使用 `doc/` 作为文档根目录，并按职责分为 `rules/`、`specs/` 和 `knowledge/`。

## 文档地图

| 文档 | 类别 | 职责 | 适用任务 |
| --- | --- | --- | --- |
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
| [`../cmd/README.md`](../cmd/README.md) | 操作说明 | MongoDB 测试数据初始化 | 仅明确授权的数据初始化任务 |
| [`../tests/bench/README.md`](../tests/bench/README.md) | 操作说明 | 历史压测脚本使用方法 | 非生产压测，需先校验请求契约 |

## 渐进加载规则

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
