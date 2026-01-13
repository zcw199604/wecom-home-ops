# 变更历史索引

本文件记录所有已完成变更的索引，便于追溯和查询。

---

## 索引

| 时间戳 | 功能名称 | 类型 | 状态 | 方案包路径 |
|--------|----------|------|------|------------|
| 202601120816 | wecom_unraid | 功能 | ✅已完成 | [202601120816_wecom_unraid](2026-01/202601120816_wecom_unraid/) |
| 202601120954 | cicd_dockerhub | 功能 | ✅已完成 | [202601120954_cicd_dockerhub](2026-01/202601120954_cicd_dockerhub/) |
| 202601121216 | unraid_container_inspect | 功能 | ✅已完成 | [202601121216_unraid_container_inspect](2026-01/202601121216_unraid_container_inspect/) |
| 202601121219 | wecom_service_framework | 功能 | ✅已完成 | [202601121219_wecom_service_framework](2026-01/202601121219_wecom_service_framework/) |
| 202601121409 | token_state_hardening | 修复 | ✅已完成 | [202601121409_token_state_hardening](2026-01/202601121409_token_state_hardening/) |
| 202601121424 | stability_refactor | 重构 | ✅已完成 | [202601121424_stability_refactor](2026-01/202601121424_stability_refactor/) |
| 202601121524 | wecom_callback_guard | 修复 | ✅已完成 | [202601121524_wecom_callback_guard](2026-01/202601121524_wecom_callback_guard/) |
| 202601121538 | test_suite | 测试 | ✅已完成 | [202601121538_test_suite](2026-01/202601121538_test_suite/) |
| 202601130553 | rename_wecom_home_ops | 重构 | ✅已完成 | [202601130553_rename_wecom_home_ops](2026-01/202601130553_rename_wecom_home_ops/) |
| 202601130629 | wecom_interaction_kb | 文档 | ✅已完成 | [202601130629_wecom_interaction_kb](2026-01/202601130629_wecom_interaction_kb/) |
| 202601130730 | wecom_autoreply_test | 测试 | ✅已完成 | [202601130730_wecom_autoreply_test](2026-01/202601130730_wecom_autoreply_test/) |

---

## 按月归档

### 2026-01

- [202601120816_wecom_unraid](2026-01/202601120816_wecom_unraid/) - 企业微信对接 Unraid 容器管理（MVP）
- [202601120954_cicd_dockerhub](2026-01/202601120954_cicd_dockerhub/) - GitHub Actions 构建并推送 Docker Hub 镜像
- [202601121216_unraid_container_inspect](2026-01/202601121216_unraid_container_inspect/) - 容器查看：状态/运行时长/资源使用/最新日志
- [202601121219_wecom_service_framework](2026-01/202601121219_wecom_service_framework/) - 企业微信多服务框架 + 青龙(QL)对接
- [202601121409_token_state_hardening](2026-01/202601121409_token_state_hardening/) - Token 并发刷新治理 + StateStore 过期清理
- [202601121424_stability_refactor](2026-01/202601121424_stability_refactor/) - Unraid 去 introspection + 配置化超时 + 回调错误处理
- [202601121524_wecom_callback_guard](2026-01/202601121524_wecom_callback_guard/) - 回调保护：请求体上限 + 短期去重（吸收重试）
- [202601121538_test_suite](2026-01/202601121538_test_suite/) - 全功能测试用例补齐 + Claude 复审
- [202601130553_rename_wecom_home_ops](2026-01/202601130553_rename_wecom_home_ops/) - 项目更名为 wecom-home-ops（module/二进制/Docker/文档对齐）
- [202601130629_wecom_interaction_kb](2026-01/202601130629_wecom_interaction_kb/) - 企业微信自建应用会话交互速查表（回调/发消息/模板卡片按钮）
- [202601130730_wecom_autoreply_test](2026-01/202601130730_wecom_autoreply_test/) - 企业微信收发自检自动回复（ping/自检）
