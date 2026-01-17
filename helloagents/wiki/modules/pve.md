# pve

## 目的
封装 Proxmox VE（PVE）API 的资源查询、VM/LXC 日常管理与告警推送能力，对外作为 Provider 接入企业微信会话交互。

## 模块概述
- **职责:** 多实例管理；PVE API 调用封装（api2/json + API Token）；资源概览（节点/存储）；VM/LXC 启停（启动/关机/重启/强制停止）；阈值告警轮询（CPU/内存/存储）+ 冷却/静默
- **状态:** 🚧开发中
- **最后更新:** 2026-01-17

## 规范

### 需求: 多实例支持
**模块:** pve
允许配置多个 PVE 实例，在企业微信会话中先选择实例，再进行查询/管理/告警操作。

#### 场景: 选择实例
用户进入 PVE 菜单后，选择目标实例。
- 实例必须来自配置白名单（`pve.instances`）
- 实例 id 仅允许字母数字及 `_ -`，且长度≤32

### 需求: 资源与健康查询（节点/存储）
**模块:** pve
支持在企业微信中查看 PVE 的资源概览：
- 节点：在线状态、CPU 使用率、内存使用率
- 存储：使用率列表（按使用率降序展示）

### 需求: VM/LXC 日常管理
**模块:** pve
首期支持 VM 与 LXC 的常用动作：
- 启动（start）
- 关机（shutdown）
- 重启（reboot）
- 强制停止（stop）

并要求二次确认，避免误触导致业务中断。

### 需求: 告警与通知闭环（阈值 + 冷却 + 静默）
**模块:** pve
支持后台轮询指标并推送告警到白名单用户（`auth.allowed_userids`）：
- CPU 使用率 > 阈值
- 内存使用率 > 阈值
- 存储使用率 > 阈值

并提供：
- **cooldown（冷却）**：同类告警在冷却窗口内最多发送一次
- **mute（静默）**：通过企业微信菜单手动静默指定实例告警一段时间（默认 `pve.alert.mute_for`）

### 需求: 文本交互兼容（微信/不支持模板卡片按钮的客户端）
**模块:** pve
当客户端无法操作模板卡片按钮时，仍需可用的“纯文本”交互路径：
- 设置 `wecom.template_card_mode: both`（卡片+文本）或 `text`（仅文本）
- 按提示 **回复序号**，映射到同等 `EventKey` 触发后续流程

## API接口
本模块不直接对外提供 HTTP API，通过内部接口供 core 调用；对 PVE 侧通过 REST API 发起 HTTPS 请求（`/api2/json/...`）。

### 官方文档参考
- `helloagents/wiki/pve_api.md` - PVE API 要点整理（鉴权/API Viewer/pvesh）

## 数据模型
- `pve.instances[].id`
- `pve.instances[].name`
- `pve.instances[].base_url`
- `pve.instances[].api_token`
- `pve.instances[].insecure_skip_verify`
- `pve.alert.*`（enabled/interval/cooldown/mute_for/阈值）

## 依赖
- core（Provider 接口与会话状态）
- wecom（卡片与文本消息交互）

## 变更历史
- [202601171251_pve_wecom](../../history/2026-01/202601171251_pve_wecom/) - PVE 接入企业微信（资源查询 / VM&LXC 管理 / 告警通知）

