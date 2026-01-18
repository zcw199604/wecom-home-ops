# 任务清单: 模板卡片模式下容器选择卡片化（Unraid）

目录: `helloagents/plan/202601181902_wecom_unraid_container_select_card/`

## 1. 卡片交互优化
- [√] 1.1 新增容器选择卡片与分页 EventKey（最多 6 按钮约束）
- [√] 1.2 Unraid：点击重启/停止/强制更新后，进入“选择容器”卡片流程（不再提示输入容器名）
- [√] 1.3 Unraid：选择容器后进入确认卡片；查看类动作可直接回显（日志默认 50 行）

## 2. 测试与回归
- [√] 2.1 更新/补充 Provider 单测覆盖容器选择卡片事件链路

## 3. 知识库同步
- [√] 3.1 更新 `wiki/modules/unraid.md` 交互说明
- [√] 3.2 更新 `CHANGELOG.md`（Unreleased）

## 4. 收尾
- [√] 4.1 迁移方案包至 `helloagents/history/2026-01/` 并更新 `helloagents/history/index.md`
