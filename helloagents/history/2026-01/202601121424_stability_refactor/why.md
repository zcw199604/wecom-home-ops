# 变更提案: 稳定性与可维护性优化（Unraid 简化 + 配置化超时 + 错误处理）

## 需求背景
近期代码审阅指出以下稳定性/可维护性问题：
- Unraid Client 为了兼容不同 GraphQL Schema，引入了较复杂的 introspection 与字段猜测逻辑，代码体积大且维护成本高。
- 关键运行参数（HTTP Client 超时、ReadHeaderTimeout、StateStore TTL）写死在代码里，调试与部署环境差异下不易调整。
- 回调入口吞没错误（永远返回 200），上游不会重试，可能导致指令丢失或用户无感知失败。
- main 中对配置加载失败直接 panic，不利于守护进程场景的可观测性与稳定性。

## 变更内容
1. Unraid 查询能力（stats/logs/force update）回归“明确、可配置”的实现：移除 query-side introspection 与字段猜测，默认使用固定字段名，并允许通过 config 覆盖。
2. 关键超时参数配置化：HTTP Client 超时、ReadHeaderTimeout、StateStore TTL 可在 `config.yaml` 调整，并保留合理默认值。
3. 错误处理改进：
   - wecom 回调：`HandleMessage` 出错时返回 5xx 触发企业微信重试（减少消息丢失）
   - main：配置加载失败不 panic，改为结构化错误输出并退出

## 影响范围
- **模块:** unraid / core / wecom / app / config
- **文件:** `internal/unraid/client.go`、`internal/config/config.go`、`internal/app/server.go`、`internal/wecom/callback.go`、`cmd/wecom-home-ops/main.go` 等
- **配置:** `config.example.yaml` 需同步新增字段
- **行为变化:** Unraid stats/logs/force update 的默认字段名固定；如上游 Schema 不一致需显式配置覆盖

## 核心场景

### 需求: Unraid 查询简化
**模块:** unraid
在不依赖 introspection 的前提下完成：
- 状态/时长：沿用 containers 查询
- 资源/日志：使用固定字段名 + 配置覆盖；缺失字段时提示配置项

### 需求: 参数配置化
**模块:** app/config/core
不同部署环境下可通过配置调整：
- HTTP client timeout
- HTTP server read header timeout
- StateStore TTL

### 需求: 回调可靠性
**模块:** wecom
当内部处理失败时返回 5xx，允许企业微信侧重试，降低消息丢失风险。

## 风险评估
- **风险:** Unraid Schema 若与默认字段不一致，会导致 stats/logs/force update 不可用
  - **缓解:** 提供配置覆盖 + 明确错误提示（指出需要配置哪些字段）
- **风险:** 回调返回 5xx 可能导致企业微信重试，产生重复提示消息
  - **缓解:** 操作类动作仍有“确认”门槛；确认事件重复时会因状态已清除而不会重复执行
