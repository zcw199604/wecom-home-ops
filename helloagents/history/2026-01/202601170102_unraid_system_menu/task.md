# 轻量迭代任务清单：unraid_system_menu

目标：新增“系统监控”菜单入口，将“系统资源概览/详情”从“容器查看”中迁移，降低用户认知成本。

## 任务

- [√] wecom：新增 EventKey 与菜单按钮（Unraid 子菜单新增“系统监控”）
- [√] wecom：新增“系统监控”模板卡片（系统资源概览/详情）
- [√] unraid provider：新增系统监控文本菜单与状态机 Step，并处理 EventKey 路由
- [√] 更新知识库与 Changelog：记录菜单分组变化
- [√] 验证：Docker 编译通过
- [√] 验证：单元测试通过（go test ./...）
- [√] 迁移方案包至 history/ 并更新 history/index.md
- [√] 交付：git commit（中文）并 push
