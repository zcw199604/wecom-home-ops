# 变更提案: 项目更名为 wecom-home-ops

## 需求背景
仓库已更名为 `wecom-home-ops`，但代码与文档仍大量使用旧名称（Go module/import、二进制名、Dockerfile、示例命令与知识库文档）。这会导致：
1. 新同学/未来自己在使用与部署时产生困惑（仓库名与程序名不一致）。
2. Go import path 与实际仓库地址不匹配，影响可移植性与外部引用。
3. Docker 镜像/容器命名与实际仓库名不一致，降低可维护性。

## 变更内容
1. 将 Go module/import path 从旧名称统一调整为 `github.com/zcw199604/wecom-home-ops`
2. 将入口目录、二进制名、Dockerfile 入口与示例命令统一更名为 `wecom-home-ops`
3. 同步更新配置默认值、测试断言与知识库文档中的旧名称

## 影响范围
- **模块:** cmd / internal / docker / docs
- **文件:** `go.mod`、`cmd/*`、`internal/*`、`Dockerfile`、`config.example.yaml`、`README.md`、`helloagents/*`
- **API:** 无
- **数据:** 无

## 核心场景

### 需求: 命名一致性
**模块:** docs
项目对外展示名称、使用示例与仓库名保持一致。

#### 场景: 开发者本地启动
使用 `go run` 能直接启动服务，且路径与仓库名一致。
- 预期结果: `go run ./cmd/wecom-home-ops -config config.yaml` 可运行

#### 场景: Docker 启动
使用 Docker 构建与启动命令时，镜像名/容器名/入口二进制名与仓库名一致。
- 预期结果: `docker build` 与 `docker run` 示例可直接使用

## 风险评估
- **风险:** Go module 与 import path 批量替换可能遗漏，导致编译失败
- **缓解:** 全量搜索旧字符串 + `gofmt` + `go test ./...` + 复查 Dockerfile 构建命令
