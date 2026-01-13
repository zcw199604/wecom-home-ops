# 任务清单: 项目更名为 wecom-home-ops

目录: `helloagents/plan/202601130553_rename_wecom_home_ops/`

---

## 1. Go module 与入口更名
- [√] 1.1 更新 `go.mod`：`module` 改为 `github.com/zcw199604/wecom-home-ops`，并批量更新 Go 代码导入路径
- [√] 1.2 重命名入口目录：统一对齐为 `cmd/wecom-home-ops/`，同步更新构建/运行命令引用

## 2. Docker/配置/运行时标识
- [√] 2.1 更新 `Dockerfile`：二进制名、COPY 路径、ENTRYPOINT 与 build target 路径全部对齐新名称
- [√] 2.2 更新 `config.example.yaml` 与 `internal/config/config.go` 默认值：`unraid.origin` 从旧名称切换为新名称，并同步测试断言
- [√] 2.3 更新 `internal/wecom/client.go`：卡片 `task_id` 前缀对齐新名称

## 3. 文档同步
- [√] 3.1 更新 `README.md`：项目标题与 go/docker 启动示例全部对齐新名称
- [√] 3.2 更新 `helloagents/wiki/*`：图示与概览文档中的旧名称全部对齐新名称
- [√] 3.3 更新 `helloagents/history/*`：历史记录中引用到的旧名称与旧路径更新为现状，避免误导

## 4. 安全检查
- [√] 4.1 执行安全检查（按G9: 仅确认无敏感信息引入；本次为重命名不触碰密钥/权限）

## 5. 测试与一致性
- [√] 5.1 执行 `gofmt` 并运行 `go test ./...`
- [√] 5.2 全仓检查旧名称残留引用（含文档/配置/代码）

## 6. 知识库与迁移
- [√] 6.1 更新 `helloagents/CHANGELOG.md` 与 `helloagents/project.md`（如涉及示例/默认入口名）
- [√] 6.2 迁移方案包至 `helloagents/history/2026-01/202601130553_rename_wecom_home_ops/` 并更新 `helloagents/history/index.md`
