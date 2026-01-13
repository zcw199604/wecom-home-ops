# 任务清单（轻量迭代）

> 目标：补齐企业微信自建应用“应用自定义菜单（底部菜单）”能力，并提供可用的通用命令（help/sync-menu 等），让交互入口更易触发。

- [√] 获取官方接口信息（SSOT：创建/获取/删除菜单 https://developer.work.weixin.qq.com/document/path/90231）
- [√] wecom client 新增 `menu/create` 调用（复用 access_token 缓存与日志）
- [√] 提供默认菜单 `wecom.DefaultMenu()`（CLICK EventKey 与现有路由约定一致）
- [√] core 路由支持 `Event=CLICK`，并将菜单点击映射到“菜单/帮助/自检/服务入口”等能力
- [√] 新增通用命令：`帮助/help`（帮助信息）、`同步菜单/更新菜单`（一键同步应用菜单）
- [√] 入口增加 `-wecom-sync-menu`：启动前一键同步自定义菜单并退出
- [√] 更新知识库：`helloagents/wiki/wecom_interaction.md`、`helloagents/wiki/modules/wecom.md`、`helloagents/CHANGELOG.md`
- [√] 测试验证：Docker 环境运行 `go test ./...` 通过；`docker build` 编译通过

