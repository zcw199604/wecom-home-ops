# unraid

## 目的
封装 Unraid 的容器管理能力，对外提供统一动作接口（重启/停止/强制更新）。

## 模块概述
- **职责:** 连接管理（如 SSH/HTTP）；执行容器操作；错误归类与回显信息格式化；作为 Provider 接入企业微信交互
- **状态:** 🚧开发中
- **最后更新:** 2026-01-12

## 规范

### 需求: 容器操作（MVP）
**模块:** unraid
对指定容器执行：
- 重启（restart）
- 停止（stop）
- 强制更新（force update）
  - 说明：强制更新依赖 Unraid GraphQL 提供相应 mutation；代码会自动回退尝试 `updateContainer(id: PrefixedID!)` 与 `update(id: PrefixedID!)`，如仍不一致再通过配置覆盖（见下文）

### 需求: 容器查看（状态/资源/日志）
**模块:** unraid
对指定容器查看：
- 状态（state/status）与运行时长（从 `status` 字符串解析，如 `Up 3 hours`）
- 资源使用（CPU/内存/网络IO/磁盘IO/PIDs）
  - 分为“资源概览”和“资源详情”两种输出
- 最新日志（默认 50 行，支持指定行数；输出有长度截断）

#### 数据来源与兼容策略
- **状态/运行时长:**
  - 数据来源：`docker { containers { id names state status } }`
  - 运行时长：仅在 `status` 以 `Up` 开头时解析并回显
- **资源/日志:**
  - 数据来源：优先使用 Unraid Connect GraphQL 的容器字段（默认 `stats` / `logs`）
  - 兼容策略：不再做 introspection 探测；改为“固定字段 + 配置覆盖”。如上游 Schema 不一致，请在 `config.yaml` 配置：
    - `unraid.logs_field` / `unraid.logs_tail_arg` / `unraid.logs_payload_field`
    - `unraid.stats_field` / `unraid.stats_fields`
    - `unraid.force_update_mutation` / `unraid.force_update_arg` / `unraid.force_update_arg_type` / `unraid.force_update_return_fields`
  - 提示：当 GraphQL 返回字段/参数错误时，错误信息会带上可配置项提示，便于快速定位

#### 输出限制
- 日志默认 `tail=50`，可输入 `1~200` 行
- 消息长度受企业微信限制：资源详情与日志会进行截断并提示“已截断”

### 需求: 连接方式可替换
**模块:** unraid
MVP 使用 Unraid Connect 插件提供的 GraphQL API（`/graphql` + `x-api-key`），并在实现层抽象“客户端/执行器”接口，允许后续在不改业务的情况下切换：
- 其他 API 形态（不同插件/版本差异）
- CLI 模块（如 `unraid-api` 提供可用命令能力）
- SSH + Docker CLI（仅作为备选）

## API接口
本模块不直接对外提供 HTTP API，通过内部接口供 core 调用。

## 数据模型
无；仅使用 Config 中的连接参数。

## 依赖
- core（接口约定）

## 变更历史
- 2026-01-12: 基于 GraphQL API 实现容器 stop/start/restart，强制更新能力可配置
- [202601121216_unraid_container_inspect](../../history/2026-01/202601121216_unraid_container_inspect/) - 容器查看：状态/运行时长/资源使用/最新日志（按 GraphQL 能力探测）
- [202601121219_wecom_service_framework](../../history/2026-01/202601121219_wecom_service_framework/) - 迁移为 Provider 并接入服务选择菜单（保持“容器/unraid”直达入口）
- [202601121424_stability_refactor](../../history/2026-01/202601121424_stability_refactor/) - 去 introspection：固定字段 + 配置覆盖（logs/stats/force update）
