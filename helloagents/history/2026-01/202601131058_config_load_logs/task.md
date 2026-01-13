# 任务清单: 配置加载日志（脱敏）

目录: `helloagents/plan/202601131058_config_load_logs/`

---

## 1. 配置可观测性
- [√] 1.1 在 `internal/config/config.go` 的 `Load` 增加配置文件读取日志（path/cwd/size/mtime/sha256）
- [√] 1.2 在配置校验通过后输出脱敏后的配置摘要（不打印 secret/token/aes_key 明文）

## 2. 测试
- [√] 2.1 运行 `go test ./...`（Docker: `golang:1.22`）
