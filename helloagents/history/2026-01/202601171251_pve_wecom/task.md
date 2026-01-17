# 任务清单: PVE 接入企业微信（资源查询 / VM&LXC 管理 / 告警通知）

目录: `helloagents/plan/202601171251_pve_wecom/`

---

## 1. 配置（config）
- [√] 1.1 在 `internal/config/config.go` 增加 `PVEConfig`（instances/alert），并补齐默认值与校验
- [√] 1.2 更新 `config.example.yaml` 增加 PVE 配置示例与告警阈值示例

## 2. PVE API 客户端（internal/pve）
- [√] 2.1 新增 `internal/pve/client.go`：实现 `GET /version`、`GET /nodes`、`GET /nodes/{node}/status`、`GET /nodes/{node}/storage`、`GET /cluster/resources` 的封装
- [√] 2.2 新增 `internal/pve/types.go`：定义常用响应结构（统一解析 `{data: ...}`）
- [√] 2.3 安全检查：确保 token 不被日志输出；URL 拼接与输入校验到位

## 3. PVE Provider（企业微信交互）
- [√] 3.1 新增 `internal/pve/provider.go`：实现 `core.ServiceProvider`（实例选择 → 动作菜单）
- [√] 3.2 新增 PVE 卡片与 EventKey：在 `internal/wecom/message.go` 增加 PVE 相关 EventKey 与卡片构造函数
- [√] 3.3 扩展 `internal/core/state.go`：增加 PVE 交互所需的 Step/字段（如 guest type/id/node），并保持对 Unraid/青龙兼容
- [√] 3.4 实现资源查询：资源概览/存储详情的渲染输出（文本为主，必要时卡片）
- [√] 3.5 实现 VM/LXC 管理：启动/关机/重启/强制停止，强制二次确认（why.md#需求-vm-lxc-日常管理-场景-搜索并重启-vm-lxc）

## 4. 告警轮询与闭环
- [√] 4.1 新增 `internal/pve/alert.go`：轮询指标并判断阈值，支持 cooldown 与 mute
- [√] 4.2 在 `internal/app/server.go` 装配并启动告警 worker，并在 `Shutdown` 时停止
- [√] 4.3 在 PVE Provider 中增加“告警状态/静默/解除静默”入口（why.md#需求-告警与通知闭环-场景-存储水位告警处理）

## 5. 底部菜单同步
- [√] 5.1 更新 `internal/wecom/message.go` 的 `DefaultMenu()`：在“常用”下新增 `PVE` 入口（`svc.select.pve`），确保满足企业微信 3 个一级菜单限制

## 6. 测试
- [√] 6.1 新增/更新 `internal/config/config_test.go`：覆盖 pve 配置校验与“至少启用一个服务”判定
- [√] 6.2 新增 `internal/pve/provider_test.go`：覆盖关键交互流程与确认逻辑
- [√] 6.3（可选）新增 `internal/pve/client_test.go`：覆盖 URL/鉴权 header 组装（不依赖真实 PVE）

## 7. 知识库同步
- [√] 7.1 新增 `helloagents/wiki/modules/pve.md`（模块说明、配置字段、交互入口）
- [√] 7.2 更新 `helloagents/wiki/overview.md` 模块索引与快速链接
- [√] 7.3 更新 `helloagents/wiki/api.md`（如需补充对外入口说明）与 `helloagents/CHANGELOG.md`
  > 备注: 本次未涉及对外 HTTP 入口变更，`helloagents/wiki/api.md` 无需更新

## 8. 安全检查
- [√] 8.1 执行安全检查（按G9：输入校验、敏感信息处理、权限控制、误操作防护）

## 9. 质量验证
- [-] 9.1 运行测试（如环境具备 Go）：`go test ./...`
  > 备注: 当前环境缺少 Go 工具链（go 命令不可用），无法在本机运行测试

## 10. 迁移方案包
- [√] 10.1 完成后按G11迁移：移动到 `helloagents/history/YYYY-MM/` 并更新 `helloagents/history/index.md`
