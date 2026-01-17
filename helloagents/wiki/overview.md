# wecom-home-ops

> 企业微信自建应用回调 + 家庭本地服务统一中间层（Unraid/青龙/PVE）

---

## 1. 项目概述

### 目标与背景
将家庭本地服务的常用管理动作统一接入企业微信应用会话，降低“登录多个面板/切换设备/VPN”带来的操作成本，并提供可审计的执行反馈。

### 范围
- **范围内（MVP）:** 企业微信自建应用回调；交互式卡片/按钮；参数输入；Unraid 容器重启/停止/强制更新；青龙(QL)任务查询/搜索/运行/启用/禁用/日志；PVE 资源概览、VM/LXC 启停与阈值告警；执行结果回显；基础权限控制（个人）。
- **范围外（MVP 不做）:** 多租户/多管理员体系；复杂审批流；更多服务的完整接入与复杂编排能力（留作后续扩展）。

### 干系人
- **负责人:** 个人用户

---

## 2. 模块索引

| 模块名称 | 职责 | 状态 | 文档 |
|---------|------|------|------|
| core | 命令路由、会话状态、权限与审计 | 🚧开发中 | [modules/core.md](modules/core.md) |
| wecom | 回调验签与解密、消息/卡片发送、会话交互适配 | 🚧开发中 | [modules/wecom.md](modules/wecom.md) |
| unraid | Unraid 连接与容器操作适配（重启/停止/强制更新） | 🚧开发中 | [modules/unraid.md](modules/unraid.md) |
| qinglong | 青龙(QL) OpenAPI 适配（任务查询/搜索/运行/启停/日志） | 🚧开发中 | [modules/qinglong.md](modules/qinglong.md) |
| pve | Proxmox VE（PVE）API 适配（资源概览/VM&LXC 管理/告警轮询） | 🚧开发中 | [modules/pve.md](modules/pve.md) |

---

## 3. 快速链接
- [技术约定](../project.md)
- [架构设计](arch.md)
- [API 手册](api.md)
- [PVE API 要点](pve_api.md)
- [数据模型](data.md)
- [青龙 OpenAPI 调用方式](qinglong_openapi.md)
- [青龙官方文档快照](qinglong_official/README.md)
- [青龙官方接口清单（源码提取）](qinglong_official/openapi.md)
- [变更历史](../history/index.md)
