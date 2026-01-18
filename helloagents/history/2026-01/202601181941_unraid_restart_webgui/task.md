# 任务清单: Unraid 重启改为 WebGUI Events.php

目录: `helloagents/plan/202601181941_unraid_restart_webgui/`

## 1. 行为调整
- [√] 1.1 Unraid：重启容器优先使用 WebGUI Events.php（`action=restart&container=<id>`）
- [√] 1.2 当未配置 WebGUI（缺少 csrf_token）时，保留 GraphQL stop+start 作为回退

## 2. 测试与回归
- [√] 2.1 增加单测覆盖 Restart 走 WebGUI Events.php 的请求形状（GraphQL 仅用于解析容器 id）

## 3. 知识库同步
- [√] 3.1 更新 `wiki/modules/unraid.md` 与 `CHANGELOG.md`（Unreleased）

## 4. 收尾
- [√] 4.1 迁移方案包至 `helloagents/history/2026-01/` 并更新 `helloagents/history/index.md`
