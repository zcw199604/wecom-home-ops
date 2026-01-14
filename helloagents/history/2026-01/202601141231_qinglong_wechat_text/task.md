# 任务清单: 青龙微信文本交互 + 任务列表 400 修复

目录: `helloagents/plan/202601141231_qinglong_wechat_text/`

---

## 1. Qinglong OpenAPI 兼容修复
- [√] 1.1 移除 `/open/crons` 等接口的 `t` query 参数，避免 400 校验失败
- [√] 1.2 更新 OpenAPI 调用文档与排障示例（同步移除 `t`）

## 2. 微信文本交互指引
- [√] 2.1 补充 README 与模块文档：微信/不支持卡片时使用 `wecom.template_card_mode: both|text`

## 3. 验证
- [√] 3.1 运行 `go test ./...`

