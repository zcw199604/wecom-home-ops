# 任务清单: Unraid 强制更新回退兼容

目录: `helloagents/plan/202601141207_unraid_force_update_compat/`

---

## 1. 强制更新兼容性
- [√] 1.1 增强 GraphQL “Cannot query field” 识别（兼容不同转义/引号风格）
- [√] 1.2 确保 `updateContainer` 不存在时自动回退到 `update`

## 2. 文档与示例配置
- [√] 2.1 更新 `config.example.yaml` 强制更新注释，提示可改为 `update`
- [√] 2.2 更新 README 与知识库文档/Changelog

## 3. 测试
- [√] 3.1 增加单测覆盖“双重转义错误体”下的回退行为
- [√] 3.2 运行 `go test ./...`（Docker: `golang:1.22`）
