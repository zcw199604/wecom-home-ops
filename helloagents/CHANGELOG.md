# Changelog

本文件记录项目所有重要变更。
格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/),
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### 新增
- GitHub Actions：push 到 main 时构建并推送 Docker Hub 镜像
- GitHub Actions：Docker 镜像构建成功/失败企业微信通知（可选）
- 企业微信：Unraid 容器查看（状态/运行时长/资源使用/最新日志）
- 企业微信：新增 ping/自检 收发自检自动回复（用于快速验证回调收消息与发送应用消息）
- 企业微信：服务启动成功后向白名单用户发送启动成功通知（诊断信息）
- 企业微信：新增应用自定义菜单同步（menu/create）与 CLICK 事件支持，提供“帮助/同步菜单”等通用命令
- 多服务 Provider 框架：服务选择菜单 + 兼容 Unraid 直达入口
- 青龙(QL) OpenAPI 对接：多实例 + 任务查询/搜索/运行/启用/禁用/日志
- 文档：新增企业微信自建应用会话交互速查表（回调/发消息/模板卡片按钮）
- 企业微信：消费 ResponseCode 调用 update_template_card，将模板卡片按钮更新为不可点击状态
- 文档：补充青龙 OpenAPI 调用方式与排障示例
- 文档：同步青龙官方文档快照（README/LICENSE）并生成官方接口清单
- 文档：补充青龙在微信/不支持模板卡片客户端的文本菜单交互说明
- 文档：整理上游项目 `s3ppo/unraid_mobile_ui` 的功能与 GraphQL API 使用方式
- 文档：整理 Unraid 官方 API（GraphQL）文档要点（可用性/鉴权/API key/OIDC/CLI/示例 Query）
- 文档：整理 PVE（Proxmox VE）官方 API（REST）文档要点（鉴权/API Viewer/pvesh/客户端库）
- pve：新增 PVE Provider（资源概览、VM/LXC 启动/关机/重启/强制停止、CPU/内存/存储阈值告警轮询 + 冷却/静默）
- wecom：底部自定义菜单“常用”新增 PVE 入口（同步菜单后生效）
- 配置：新增 `server.http_client_timeout` / `server.read_header_timeout` / `core.state_ttl`，支持按环境调整
- 测试：补齐 Unraid/Qinglong/WeCom 交互与边界的详细单元测试
- unraid：强制更新新增 WebGUI StartCommand.php 兜底（支持 `update_container <name>`；需配置 csrf_token/可选 Cookie）
- 文档：新增目标实例 `10.10.10.100` 的 GraphQL schema 摘要（Query/Mutation/Subscription + Docker/VM/Array 等关键字段清单）

### 修复
- GitHub Actions：企业微信通知改用文本消息（text），并补充 commit message
- wecom/qinglong：token 刷新引入 singleflight，避免并发刷新击穿与上游限流风险
- core：StateStore 增加后台定时清理，避免过期状态长期驻留
- wecom：回调增加请求体上限与短期去重，吸收重试并避免重复执行业务逻辑
- wecom：模板卡片补齐 `source` 字段，修复部分客户端“发送成功但不展示”的问题
- wecom/core：新增 `wecom.template_card_mode`（template_card/both/text）与“回复序号触发 EventKey”文本兜底，解决模板卡片不展示导致菜单无响应
- core/unraid：支持菜单点击的“文本模式”兜底与文本确认（避免模板卡片不展示时无法继续）
- wecom/unraid：模板卡片模式下，容器动作选择后改为“选择容器”卡片继续交互（不再提示输入容器名）
- unraid：容器重启在配置 WebGUI `Events.php`（csrf_token/可选 Cookie）时优先走 `action=restart`（避免自重启时 stop+start 中断）
- unraid：强制更新 mutation 回退识别增强（兼容 GraphQL 错误转义差异），自动回退尝试 `updateContainer/update`
- unraid：当目标 Unraid 未提供更新相关 GraphQL mutation 时，强制更新可自动切换至 WebGUI 兜底（避免直接失败）
- qinglong：移除 OpenAPI 请求中的 `t` 时间戳 query，修复部分版本参数校验导致任务列表 400 的问题
- wecom：增强发送消息/gettoken/update_template_card 的结构化日志；message/send 返回 invaliduser 等不可达信息时输出告警并返回错误
- app/wecom：回调与请求日志增强（GET/POST 回调增加验签/解密/解析阶段日志；请求日志增加 status_code/response_bytes）
- config：配置加载日志增强（输出 config 文件 path/sha256/size/mtime；打印脱敏配置摘要用于确认容器挂载是否生效）
- main：配置加载失败不再 panic，改为日志输出并退出
- wecom：修复 PKCS7 padding blockSize 与官方一致（32），解决回调解密 invalid pkcs7 padding

### 变更
- 项目：整体更名为 wecom-home-ops（Go module/import、入口二进制、Dockerfile、示例命令与文档）
- unraid：移除 GraphQL introspection 探测逻辑，改为固定字段 + 配置覆盖（logs/stats/force update）
- unraid：系统资源概览/详情的“内存已用”改为 total-available 口径，并补充容器累计网络 IO（stats.netIO 汇总）
- unraid：系统资源概览/详情输出改为中文易读格式，优先展示关键指标
- unraid：系统资源概览补充 Unraid 启动时长与 UPS 信息（若可用）
- unraid：菜单新增“系统监控”入口，系统资源从“容器查看”迁移
- Docker：运行时镜像改为 scratch 并合并构建步骤，减少镜像层数
- 构建：升级 Go 工具链至 1.25.6（go.mod toolchain / Dockerfile）

## [0.1.0] - 2026-01-12

### 新增
- 企业微信自建应用回调验签/解密与应用消息发送（含 access_token 缓存）
- Unraid GraphQL 容器管理：重启/停止/强制更新（按 API 能力探测）
- 会话状态机：按钮选择 + 参数输入 + 二次确认
- 健康检查与基础结构化日志
