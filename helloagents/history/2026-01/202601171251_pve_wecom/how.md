# 技术设计: PVE 接入企业微信（资源查询 / VM&LXC 管理 / 告警通知）

## 技术方案

### 核心技术
- 复用现有 Provider 框架：`internal/core.ServiceProvider`
- 新增 PVE REST API 客户端：`internal/pve`（基于 `api2/json` + `Authorization: PVEAPIToken=...`）
- 告警轮询：后台 goroutine 定时拉取节点/存储指标并进行阈值判断，带冷却与静默
- 企业微信交互：模板卡片 + 文本兜底（沿用 `wecom.template_card_mode`）

### 交互设计（PVE Provider）
1. 进入 PVE
   - 多实例：实例选择卡片
   - 单实例：直接进入动作菜单
2. 动作菜单
   - 资源概览
   - VM 管理（启动/关机/重启/强制停止）
   - LXC 管理（启动/关机/重启/强制停止）
   - 告警状态（展示阈值、静默状态、最近告警时间）
   - 静默告警 / 解除静默
   - 切换实例（多实例时）
3. 目标选择
   - 输入 VMID（数字）或名称关键词
   - 关键词命中多项：最多展示前 N 个（建议 N=4）按钮供选择
   - 所有写操作统一进入 `core.StepAwaitingConfirm` 的二次确认卡片

### 告警策略（首期）
- 指标来源（建议优先使用稳定接口）：
  - 节点资源：`GET /nodes/{node}/status`
  - 存储水位：`GET /nodes/{node}/storage`（或 `GET /cluster/resources` 中的 storage 项）
- 阈值（百分比）：
  - CPU 使用率阈值：`pve.alert.cpu_usage_threshold`
  - 内存使用率阈值：`pve.alert.mem_usage_threshold`
  - 存储使用率阈值：`pve.alert.storage_usage_threshold`
- 轮询间隔：`pve.alert.interval`
- 冷却时间：`pve.alert.cooldown`（同一实例+同一告警键在 cooldown 内最多推送一次）
- 静默：`pve.alert.mute_for`（通过 PVE Provider 设置，内存态）

### 菜单同步（方案1）
- 企业微信底部菜单保持 3 个一级菜单不变（满足企业微信限制）
- 在“常用”下新增一个二级按钮：`PVE` → `svc.select.pve`

## 架构决策 ADR

### ADR-004: PVE 接入沿用 Provider 框架（已采纳）
**上下文:** 已有 Unraid/青龙 Provider、白名单鉴权与会话状态机，需要新增 PVE 并支持多实例与告警。  
**决策:** 新增 `internal/pve` 包并实现 `pve.Provider`，在 `app.NewServer` 中按配置注册到 `core.Router`。  
**理由:** 复用既有交互与分发机制，新增服务成本最低；对 core 改动可控。  
**替代方案:** 在 core/router 内堆叠分支 → 拒绝原因: 随服务数量增长不可维护。  
**影响:** 需要扩展 config/schema 与 wecom menu；新增后台任务需处理 shutdown。

## 安全与性能
- **安全:**
  - 强制二次确认：所有 VM/LXC 写操作必须走 `StepAwaitingConfirm`
  - 仅白名单用户可用（沿用 `auth.allowed_userids`）
  - PVE API Token 不写日志/不回显；配置脱敏日志只输出长度/是否配置
  - 事件 key 与用户输入（VMID/关键词/实例 ID）做严格校验，避免注入与越权
- **性能:**
  - 告警轮询默认低频（如 1m-5m），单次请求量按节点数线性增长
  - 目标搜索与映射优先走 `/cluster/resources`，减少多次探测

## 测试与部署
- **测试:**
  - config 校验单元测试：新增 pve 配置字段与默认值
  - PVE Provider 单测：交互流程、确认逻辑、事件 key 解析、文本兜底
  - wecom.DefaultMenu 单测（如有相关覆盖）：确保菜单结构合法
- **部署:**
  - 更新 `config.example.yaml` 增加 pve 配置示例
  - 启动后执行“同步菜单”应用到底部菜单
