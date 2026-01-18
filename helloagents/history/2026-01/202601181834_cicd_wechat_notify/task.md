# 轻量迭代任务清单：CI 企业微信构建通知

目标：在 GitHub Actions 构建并推送 Docker Hub 镜像后，按成功/失败发送企业微信 Markdown 通知；未配置时自动跳过。

## 任务
- [√] 在 `.github/workflows/dockerhub.yml` 增加构建成功通知（可选，失败不影响 job）
- [√] 在 `.github/workflows/dockerhub.yml` 增加构建失败通知（可选，失败不影响 job）
- [√] 更新知识库 `helloagents/project.md`：补充企业微信通知相关 Secrets 说明
- [√] 更新 `helloagents/CHANGELOG.md`：记录新增 CI 通知能力
- [√] 校验 YAML 语法（PyYAML 解析通过）

## 备注
- 通知开关：未配置 `WECHAT_CORP_ID` 时自动跳过，不影响构建
- Secrets：均通过 GitHub Secrets 注入，避免明文落盘
