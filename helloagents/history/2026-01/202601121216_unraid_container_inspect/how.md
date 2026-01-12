# 技术设计: Unraid 容器状态/资源/日志查看

## 技术方案

### 核心技术
- Go 1.22 + `net/http`
- 企业微信自建应用消息回调（template card + 文本消息）
- Unraid Connect GraphQL API（`/graphql` + `x-api-key`）
- GraphQL introspection（运行时探测 stats/logs 能力）

### 实现要点
- **交互层（core/wecom）**
  - “容器”入口提供二级菜单：`容器操作` 与 `容器查看`，避免单卡按钮数过多导致兼容性问题。
  - 继续保留「选择动作 → 输入容器名」流程；操作类动作保留二次确认；查看类动作输入后直接回显结果。
  - 文本命令直达：`状态/资源/详情/日志`，并兼容 `日志 <name> <lines>`（默认 50，限制最大值防止消息过长）。
  - 统一输出长度控制：日志按行截断、整体按字符/字节截断，尾部追加“已截断”提示。

- **数据层（unraid）**
  - 复用现有 `docker { containers { id names state status } }` 查询作为“状态/运行时长”的数据来源，运行时长从 `status` 字符串解析得到。
  - stats/logs 通过 introspection 探测：
    - 先定位 `docker` Query 类型与 `containers` 返回的容器类型（如 `DockerContainer`）。
    - 在容器类型上寻找候选字段（stats/logs），并根据字段参数自动选择 `tail/lines/limit` 等入参名称（如支持）。
    - 若字段不存在或不可用，返回结构化“能力不支持”错误，由 core 转译成用户可读提示。
  - 资源字段解析策略：
    - **概览**：从探测到的 stats 字段中优先匹配常见指标（CPU/内存/网络/磁盘IO/PIDs）；匹配不到时降级为“原始字段列表输出”。
    - **详情**：尽可能输出完整 stats 字段（仍受消息长度限制）。

## 架构设计
无新增外部组件；在现有 `core → unraid` 调用链中增加“查询类动作”。

## 架构决策 ADR
### ADR-001: 方案1采用纯 GraphQL，不引入 SSH 兜底
**上下文:** stats/logs 能力存在版本差异，但用户选择方案1（最小改动）。
**决策:** 仅通过 GraphQL + introspection 实现，缺失能力时返回明确提示，不增加 SSH 凭据与连接逻辑。
**理由:** 变更范围可控；安全面更小；与现有 MVP 连接方式一致。
**替代方案:** GraphQL + SSH 兜底 → 拒绝原因: 用户未选择，且引入凭据管理与更多安全风险。
**影响:** 若插件版本不支持 stats/logs，则相关动作只能提示“不支持”，但不影响状态查看与操作类动作。

## API设计
无对外 HTTP API 变更；仅扩展内部接口：
- `unraid.Client` 增加 `GetContainerStatus/Stats/Logs` 等方法
- `core.Router` 增加对应 Action 分支与消息输出

## 安全与性能
- **安全:** 继续使用 `auth.allowed_userids` 白名单控制；不记录敏感日志内容；日志回显做长度限制与失败提示。
- **性能:** 查询类动作设置超时；避免过大日志拉取；必要时对 introspection 结果做短期缓存（按 TTL）。

## 测试与部署
- **测试:** 为 `unraid` 新增 httptest 覆盖：能力探测、logs/stats 查询构造、错误转译；为 `core` 新增命令解析与查看类动作流程测试。
- **部署:** 无需新增基础设施；升级镜像后生效。
