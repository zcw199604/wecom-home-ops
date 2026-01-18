# unraid

## 目的
封装 Unraid 的容器管理能力，对外提供统一动作接口（重启/停止/强制更新）。

## 模块概述
- **职责:** 连接管理（如 SSH/HTTP）；执行容器操作；错误归类与回显信息格式化；作为 Provider 接入企业微信交互
- **状态:** 🚧开发中
- **最后更新:** 2026-01-18

## 规范

### 需求: 容器操作（MVP）
**模块:** unraid
对指定容器执行：
- 重启（restart）
- 停止（stop）
- 强制更新（force update）
  - 说明：优先使用 Unraid GraphQL 提供相应 mutation；代码会自动回退尝试 `updateContainer(id: PrefixedID!)` 与 `update(id: PrefixedID!)`，如仍不一致再通过配置覆盖（见下文）
  - 兜底：当目标 Unraid 的 `DockerMutations` 不提供更新相关 mutation（GraphQL 校验错误）时，可启用 WebGUI StartCommand.php 兜底执行 `update_container <name>`（需配置 csrf_token，可能需要 Cookie）

**交互（企业微信会话）:**
- 模板卡片模式（`wecom.template_card_mode: template_card|both`）：动作选择后发送“选择容器”卡片，点击容器进入确认卡片（支持分页）。
- 文本模式（`wecom.template_card_mode: text`）：按提示输入容器名（保留兼容）。

### 需求: 容器查看（状态/日志）
**模块:** unraid
对指定容器查看：
- 状态（state/status）与运行时长（从 `status` 字符串解析，如 `Up 3 hours`）
- 最新日志（默认 50 行，支持指定行数；输出有长度截断）

**交互（企业微信会话）:**
- 模板卡片模式：动作选择后发送“选择容器”卡片；日志默认拉取 50 行并回显。
- 文本模式：可输入 `容器名 [行数]`（默认 50 行，最大 200 行）。

### 需求: 系统监控（系统资源概览/详情）
**模块:** unraid
查看主机系统资源（入口位于企业微信 Unraid 菜单的“系统监控”，不在“容器查看”中展示）：
- CPU：使用 `metrics.cpu.percentTotal`（详情额外展示 per-CPU 前 8 个核）
- 内存：概览展示使用 `effective used = total - available` 的口径；详情同时保留 raw used 便于排查
- 启动时长：可选展示 Unraid 系统运行时长（`info { os { uptime } }`；通常为秒）
- 网络：可选展示“容器累计网络 IO”（汇总所有容器 `stats.netIO` 的 rx/tx；若目标 schema/config 不支持则自动省略）
- UPS：可选展示 UPS 状态/电量/续航/负载（来自 `upsDevices { ... }`；未配置或无设备会提示“未检测到/未获取到”）
- 输出：中文标签，优先把关键指标放在前面，便于微信端快速判读
- 5 分钟均值：暂不实现（如需要需在客户端侧采样聚合）

#### 数据来源与兼容策略
- **状态/运行时长:**
  - 数据来源：`docker { containers { id names state status } }`
  - 运行时长：仅在 `status` 以 `Up` 开头时解析并回显
- **系统资源:**
  - CPU/内存：`metrics { cpu { ... } memory { ... } }`
  - 启动时长：`info { os { uptime } }`
  - 网络（容器累计）：`docker { containers { <stats_field> { netIO } } }`（依赖 `unraid.stats_field`，且字段需要支持 `netIO`）
  - UPS：`upsDevices { ... }`（如 query 不支持会自动跳过并提示“未获取到”）
- **日志:**
  - 数据来源：Unraid Connect GraphQL 的容器字段（默认 `logs`）
  - 兼容策略：不再做 introspection 探测；改为“固定字段 + 配置覆盖”。如上游 Schema 不一致，请在 `config.yaml` 配置：
    - `unraid.logs_field` / `unraid.logs_tail_arg` / `unraid.logs_payload_field`
    - `unraid.stats_field` / `unraid.stats_fields`
    - `unraid.force_update_mutation` / `unraid.force_update_arg` / `unraid.force_update_arg_type` / `unraid.force_update_return_fields`
    - （可选兜底）`unraid.webgui_csrf_token` / `unraid.webgui_cookie` / `unraid.webgui_command_url`
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

## 参考
- [unraid_official_api](../unraid_official_api.md) - Unraid 官方 API（GraphQL）要点整理（版本可用性/API key/OIDC/CLI/示例 Query）
- [unraid_mobile_ui](../unraid_mobile_ui.md) - Flutter 客户端项目：功能清单与 GraphQL 使用方式整理（/graphql + x-api-key + ws/wss subscription）
- [unraid_schema_10.10.10.100](../unraid_schema_10.10.10.100.md) - 目标实例 GraphQL schema 摘要（含 DockerMutations/Array/Vm 等可用字段清单）

## 变更历史
- 2026-01-12: 基于 GraphQL API 实现容器 stop/start/restart，强制更新能力可配置
- 2026-01-14: 强制更新回退识别增强，兼容 GraphQL 错误转义差异，避免未触发回退
- 2026-01-14: 强制更新新增 WebGUI StartCommand.php 兜底（update_container）
- 2026-01-17: 系统资源：内存已用按 total-available 口径展示，并补充容器累计网络 IO（stats.netIO 汇总）
- 2026-01-17: 系统资源概览：补充 Unraid 启动时长与 UPS 信息（若可用）
- 2026-01-17: 菜单：新增“系统监控”入口，系统资源从“容器查看”迁移
- 2026-01-18: 企业微信模板卡片：容器动作选择后通过“选择容器”卡片继续交互（减少手动输入）
- [202601141207_unraid_force_update_compat](../../history/2026-01/202601141207_unraid_force_update_compat/) - 强制更新回退兼容（错误转义差异）
- [202601141334_unraid_force_update_webgui_fallback](../../history/2026-01/202601141334_unraid_force_update_webgui_fallback/) - 强制更新 WebGUI 兜底（StartCommand.php update_container）
- [202601121216_unraid_container_inspect](../../history/2026-01/202601121216_unraid_container_inspect/) - 容器查看：状态/运行时长/资源使用/最新日志（按 GraphQL 能力探测）
- [202601121219_wecom_service_framework](../../history/2026-01/202601121219_wecom_service_framework/) - 迁移为 Provider 并接入服务选择菜单（保持“容器/unraid”直达入口）
- [202601121424_stability_refactor](../../history/2026-01/202601121424_stability_refactor/) - 去 introspection：固定字段 + 配置覆盖（logs/stats/force update）
