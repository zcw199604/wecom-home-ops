# 任务清单: GitHub Actions 构建并推送 Docker Hub 镜像

目录: `helloagents/plan/202601120954_cicd_dockerhub/`

---

## 1. 容器化
- [√] 1.1 新增 `Dockerfile`（多阶段构建 + 最小运行镜像），确保可构建 `daily-help` 二进制
- [√] 1.2 新增 `.dockerignore` 减少构建上下文体积

## 2. CI/CD
- [√] 2.1 新增 GitHub Actions 工作流：push 到 `main` 时构建并推送到 Docker Hub（使用 Secrets 登录）

## 3. 文档与变更记录
- [√] 3.1 更新 `helloagents/project.md` 补充 CI/CD 与 Secrets 约定
- [√] 3.2 更新 `helloagents/CHANGELOG.md`（Unreleased）记录新增 CI/CD

## 4. 验证
- [√] 4.1 本地静态验证：`docker build`（可选）与 workflow 语法检查（结构自检）
