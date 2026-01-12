# Changelog

本文件记录项目所有重要变更。
格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/),
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### 新增
- GitHub Actions：push 到 main 时构建并推送 Docker Hub 镜像
- 企业微信：Unraid 容器查看（状态/运行时长/资源使用/最新日志）
- 多服务 Provider 框架：服务选择菜单 + 兼容 Unraid 直达入口
- 青龙(QL) OpenAPI 对接：多实例 + 任务查询/搜索/运行/启用/禁用/日志
- 配置：新增 `server.http_client_timeout` / `server.read_header_timeout` / `core.state_ttl`，支持按环境调整

### 修复
- wecom/qinglong：token 刷新引入 singleflight，避免并发刷新击穿与上游限流风险
- core：StateStore 增加后台定时清理，避免过期状态长期驻留
- wecom：回调增加请求体上限与短期去重，吸收重试并避免重复执行业务逻辑
- main：配置加载失败不再 panic，改为日志输出并退出

### 变更
- unraid：移除 GraphQL introspection 探测逻辑，改为固定字段 + 配置覆盖（logs/stats/force update）

## [0.1.0] - 2026-01-12

### 新增
- 企业微信自建应用回调验签/解密与应用消息发送（含 access_token 缓存）
- Unraid GraphQL 容器管理：重启/停止/强制更新（按 API 能力探测）
- 会话状态机：按钮选择 + 参数输入 + 二次确认
- 健康检查与基础结构化日志
