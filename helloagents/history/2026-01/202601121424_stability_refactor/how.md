# 技术设计: 稳定性与可维护性优化（Unraid 简化 + 配置化超时 + 错误处理）

## 技术方案

### 1) Unraid：移除 query-side introspection
- 移除 `detectContainerInspectMeta/lookupDockerQueryTypeName/build*FieldExpr` 等动态探测与字段猜测逻辑。
- 改为固定 Query 模板：
  - 状态：`docker { containers { id names state status } }`
  - 日志：默认 `logs(tail: $tail)`（字段名/参数名可配置）
  - 资源：默认 `stats { cpuPercent memUsage memLimit netIO blockIO pids }`（字段名/子字段可配置）
- 配置校验：新增字段名必须满足 GraphQL identifier 规则（避免拼接导致注入风险）。
- force update：
  - 默认 mutation 为 `update`，参数名默认 `id`（可配置覆盖）
  - 失败时给出明确提示，引导用户在配置中调整

### 2) 参数配置化
- 在 `internal/config/config.go` 引入可解析的 Duration 配置类型（YAML 支持 `15s/30m`）。
- 新增配置项并提供默认值：
  - `server.http_client_timeout`
  - `server.read_header_timeout`
  - `core.state_ttl`

### 3) 回调与启动错误处理
- `internal/wecom/callback.go`：
  - 如果 `HandleMessage` 返回 error，HTTP 返回 500（触发企业微信重试）
- `cmd/wecom-home-ops/main.go`：
  - 配置加载失败：输出错误并 `os.Exit(1)`，不 panic

## 安全与性能
- 对“可配置的 GraphQL 字段名/参数名”做白名单校验（identifier），降低注入风险。
- 移除 introspection 后减少启动期 schema 请求，降低复杂度与外部依赖。

## 测试与验证
- `go test ./...` 全量通过
- 增加 Unraid client 的配置覆盖单测（字段名校验、缺失字段提示）
