# 任务清单: 企业微信收发自检自动回复

目录: `helloagents/plan/202601130730_wecom_autoreply_test/`

---

## 1. 收发自检逻辑
- [√] 1.1 在 `internal/core/router.go` 增加自检指令（`ping`/`自检`）判定与自动回复（`pong` + 诊断字段）
- [√] 1.2 在 `internal/core/router_test.go` 增加自检自动回复单元测试

## 2. 文档更新
- [√] 2.1 更新 `helloagents/wiki/wecom_interaction.md` 补充收发自检说明与代码位置
- [√] 2.2 更新 `helloagents/wiki/modules/wecom.md` 补充自检入口说明
- [√] 2.3 更新 `README.md` 补充“ping/自检”使用说明

## 3. 知识库同步
- [√] 3.1 更新 `helloagents/CHANGELOG.md`（Unreleased）记录新增收发自检能力

## 4. 测试
- [√] 4.1 运行 `go test ./...`（Docker: `golang:1.22`）
