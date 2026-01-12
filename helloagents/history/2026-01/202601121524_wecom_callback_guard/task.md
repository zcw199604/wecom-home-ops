# 任务清单: 企业微信回调保护（请求体上限 + 去重）

目录: `helloagents/plan/202601121524_wecom_callback_guard/`

---

## 1. 回调请求体大小限制
- [√] 1.1 在 `internal/wecom/callback.go` 对 POST 回调 body 使用 `http.MaxBytesReader`，超限返回 413

## 2. 回调去重/幂等（吸收重试）
- [√] 2.1 扩展 `internal/wecom/message.go`：补充 `MsgId/CreateTime` 等字段（用于构造去重 key）
- [√] 2.2 新增 `internal/wecom/deduper.go`：实现短期去重缓存（TTL + 清理）
- [√] 2.3 在 `internal/wecom/callback.go` 引入 deduper：同一消息重复回调直接返回 200，不重复调用 core
- [√] 2.4 更新 `internal/app/server.go` 装配 deduper，并在 shutdown 时关闭

## 3. 测试与文档
- [√] 3.1 新增 `internal/wecom/callback_test.go`：覆盖 body 超限与重复回调去重
- [√] 3.2 更新 `helloagents/wiki/modules/wecom.md` 与 `helloagents/CHANGELOG.md`
