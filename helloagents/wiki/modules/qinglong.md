# qinglong

## 目的
封装青龙(QL) OpenAPI 的任务管理能力，对外提供统一动作接口（查询/搜索/运行/启用/禁用/日志）。

## 模块概述
- **职责:** 多实例管理；OpenAPI token 获取与缓存；任务列表/搜索/详情/日志；任务运行与启停
- **状态:** 🚧开发中
- **最后更新:** 2026-01-12

## 规范

### 需求: 多实例支持
**模块:** qinglong
允许配置多个青龙实例，在企业微信会话中先选择实例，再执行任务操作。

#### 场景: 选择实例
用户进入青龙菜单后，选择目标实例。
- 实例必须来自配置白名单（`qinglong.instances`）
- 实例 id 仅允许字母数字及 `_ -`，且长度≤32

### 需求: 任务查询与搜索
**模块:** qinglong
支持任务列表与关键词搜索，并允许在企业微信交互中选择目标任务。

### 需求: 任务运行与启停
**模块:** qinglong
对选中任务支持：
- 运行（run）
- 启用（enable）
- 禁用（disable）
并要求二次确认。

### 需求: 查看最近日志
**模块:** qinglong
支持获取任务最近日志并在企业微信中摘要回显，避免超长消息。

### 需求: OpenAPI token 缓存与并发刷新治理
**模块:** qinglong
青龙 OpenAPI 依赖 `/open/auth/token` 获取 token：
- 本地缓存 token 与过期时间
- 并发场景使用 singleflight 合并刷新请求，避免 token 过期时并发打满上游

### 需求: 文本交互兼容（微信/不支持模板卡片按钮的客户端）
**模块:** qinglong
当客户端无法操作模板卡片按钮（例如在微信中使用）时，仍需可用的“纯文本”交互路径：
- 在 `config.yaml` 中设置 `wecom.template_card_mode: both`（卡片+文本）或 `text`（仅文本）
- 交互方式：按提示 **回复序号**，映射到同等 `EventKey` 触发后续流程

## API接口
本模块不直接对外提供 HTTP API，通过内部接口供 core 调用；对青龙侧通过 OpenAPI 发起 HTTP 请求（如 `/open/auth/token`、`/open/crons` 等）。

### OpenAPI 调用方式（整理）
- 调用约定与排障示例：`helloagents/wiki/qinglong_openapi.md`
- 代码实现参考：`internal/qinglong/client.go`

### 官方文档（本地快照）
- 上游 README：`helloagents/wiki/qinglong_official/README.md`
- 上游 LICENSE：`helloagents/wiki/qinglong_official/LICENSE`
- 官方接口清单（源码提取）：`helloagents/wiki/qinglong_official/openapi.md`

## 数据模型
- `qinglong.instances[].base_url`
- `qinglong.instances[].client_id`
- `qinglong.instances[].client_secret`

## 依赖
- core（Provider 接口与会话状态）
- wecom（卡片与文本消息交互）

## 变更历史
- [202601121219_wecom_service_framework](../../history/2026-01/202601121219_wecom_service_framework/) - 企业微信多服务框架 + 青龙(QL)对接
- [202601141231_qinglong_wechat_text](../../history/2026-01/202601141231_qinglong_wechat_text/) - 微信文本菜单交互指引 + 任务列表 400 修复
- 2026-01-12: OpenAPI token 刷新引入 singleflight，抑制并发刷新击穿
