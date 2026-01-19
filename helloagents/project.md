# 项目技术约定

## 技术栈
- **语言:** Go（建议 ≥ 1.25.6）
- **HTTP 服务:** `net/http`（可选 `chi` 路由）
- **配置:** 环境变量 + 配置文件（YAML）
- **日志:** 结构化日志（JSON），按请求链路输出关键字段

## 开发约定
- **代码规范:** `gofmt` 必须通过
- **命名约定:** Go 官方风格（导出标识符使用 UpperCamelCase，包名小写）
- **目录结构建议:** `cmd/` + `internal/`，对外可复用放 `pkg/`

## 错误与日志
- **策略:** 只在边界层（HTTP 入口）统一转译错误码；内部返回 `error` 并携带上下文
- **日志字段:** `request_id`, `wecom_userid`, `action`, `target`, `result`, `duration_ms`

## 测试与流程
- **测试:** 至少提供关键单元测试（加解密校验、签名校验、路由与状态机）
- **命令:** `go test ./...`
- **提交:** 建议每次提交聚焦单一主题（初始化/功能/修复/文档）

## CI/CD 与镜像发布
- **Docker 镜像构建:** `Dockerfile`（多阶段构建，默认入口 `-config /config/config.yaml`）
- **GitHub Actions:** push 到 `main` 时自动构建并推送 Docker Hub（见 `.github/workflows/dockerhub.yml`）；可选发送企业微信构建成功/失败通知（未配置则自动跳过）
- **必需 Secrets:**
  - `DOCKERHUB_USERNAME`: Docker Hub 用户名
  - `DOCKERHUB_TOKEN`: Docker Hub Access Token（建议使用 Token 而非密码）
- **可选 Secrets:**
  - `DOCKERHUB_REPOSITORY`: 完整镜像名（如 `yourname/wecom-home-ops`）；未设置时默认 `DOCKERHUB_USERNAME/<GitHub仓库名>`
  - `WECHAT_CORP_ID`: 企业微信 CorpID（启用通知用）
  - `WECHAT_CORP_SECRET`: 企业微信应用 Secret（启用通知用）
  - `WECHAT_AGENT_ID`: 企业微信应用 AgentId（启用通知用）
  - `WECHAT_TO_USER`: 接收通知的 userid，多个用 `|` 分隔（如 `zhangsan|lisi`）
  - `WECHAT_API_URL`: 企业微信 API 地址（可选，默认 `https://qyapi.weixin.qq.com`，用于代理/私有化部署）
