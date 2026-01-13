# 技术设计: 项目更名为 wecom-home-ops

## 技术方案
### 核心技术
- Go module/import path 重命名
- `git mv` 重命名入口目录
- Dockerfile 二进制名/入口更新
- 文档与配置的全量一致性更新

### 实现要点
- Go module：
  - `go.mod` 的 `module` 改为 `github.com/zcw199604/wecom-home-ops`
  - 所有旧导入路径统一改为 `github.com/zcw199604/wecom-home-ops/internal/...`
- 入口目录与构建：
  - 入口目录统一对齐为 `cmd/wecom-home-ops/`
  - Dockerfile 中 `go build` 的输出文件名与入口路径同步修改
- 运行时标识：
  - 统一替换卡片 `task_id` 前缀、Unraid `origin` 默认值等旧名称
- 文档：
  - `README.md`、`helloagents/wiki/*`、`helloagents/history/*` 中的旧名称引用保持与代码一致

## 安全与性能
- **安全:** 仅重命名与文档同步，不涉及密钥/权限逻辑变更
- **性能:** 无影响

## 测试与部署
- **测试:** `gofmt` + `go test ./...` + 全仓检查旧名称残留引用
- **部署:** Dockerfile 更新后可继续使用现有 CI 构建推送；运行时命令以新二进制名/路径为准
