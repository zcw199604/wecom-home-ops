# 任务清单: 稳定性与可维护性优化（Unraid 简化 + 配置化超时 + 错误处理）

目录: `helloagents/plan/202601121424_stability_refactor/`

---

## 1. Unraid：移除 introspection + 配置化字段
- [√] 1.1 在 `internal/unraid/client.go` 中移除 query-side introspection，并改为固定 query 模板（logs/stats/force update 支持配置覆盖）
- [√] 1.2 在 `internal/config/config.go` 中扩展 `UnraidConfig`：新增 logs/stats/force update 字段配置并做 identifier 校验
- [√] 1.3 补充 `internal/unraid/client_test.go`：覆盖默认字段与配置覆盖、错误提示可读性

## 2. 参数配置化
- [√] 2.1 在 `internal/config/config.go` 增加 Duration 类型与默认值：`server.http_client_timeout`、`server.read_header_timeout`、`core.state_ttl`
- [√] 2.2 更新 `internal/app/server.go` 使用配置项（替代硬编码 15s/10s/30m）
- [√] 2.3 更新 `config.example.yaml`

## 3. 错误处理改进
- [√] 3.1 在 `internal/wecom/callback.go` 中处理 `HandleMessage` 返回 error：返回 5xx 触发重试
- [√] 3.2 在 `cmd/wecom-home-ops/main.go` 中移除 `panic`，改为日志输出 + 退出码

## 4. 安全检查
- [√] 4.1 安全检查：配置字段校验、避免敏感信息输出、避免重复执行风险

## 5. 文档与变更记录
- [√] 5.1 更新 `helloagents/wiki/modules/unraid.md`（配置项与默认行为）
- [√] 5.2 更新 `helloagents/CHANGELOG.md`

## 6. 测试
- [√] 6.1 运行 `go test ./...`（使用临时 Go 1.22.8 工具链）
