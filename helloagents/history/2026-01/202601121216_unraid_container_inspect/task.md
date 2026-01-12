# 任务清单: Unraid 容器状态/资源/日志查看

目录: `helloagents/plan/202601121216_unraid_container_inspect/`

---

## 1. unraid（查询能力）
- [√] 1.1 在 `internal/unraid/client.go` 中新增“Query side”introspection：定位 docker query 类型、容器类型、并探测 stats/logs 字段，验证 why.md#需求:-资源概览 与 why.md#需求:-最新日志
- [√] 1.2 在 `internal/unraid/client.go` 中实现 `GetContainerStatusByName`（基于 `containers` 查询，并从 status 解析运行时长），验证 why.md#需求:-状态查看
- [√] 1.3 在 `internal/unraid/client.go` 中实现 `GetContainerStatsByName`（概览/详情两种输出策略，缺失能力时返回可读错误），验证 why.md#需求:-资源概览 与 why.md#需求:-资源详情，依赖任务1.1
- [√] 1.4 在 `internal/unraid/client.go` 中实现 `GetContainerLogsByName`（默认 50 行，支持传入行数；缺失能力时返回可读错误），验证 why.md#需求:-最新日志，依赖任务1.1
- [√] 1.5 在 `internal/unraid/client_test.go` 中补充单测：模拟 introspection + logs/stats 返回，覆盖“支持/不支持”两条路径，验证点：能正确探测并给出稳定错误信息

## 2. wecom（卡片交互）
- [√] 2.1 在 `internal/wecom/message.go` 中新增“容器查看”菜单卡片与事件 key（状态/资源概览/资源详情/日志），验证 why.md#需求:-状态查看 与 why.md#需求:-资源详情

## 3. core（动作路由与文本命令）
- [√] 3.1 在 `internal/core/state.go` 中扩展 Action 枚举与显示名，并区分“操作类需要确认/查看类无需确认”，验证 why.md#需求:-状态查看
- [√] 3.2 在 `internal/core/router.go` 中实现文本命令解析：`状态/资源/详情/日志`（含可选行数），并在查看类动作中输出格式化结果；日志与详情做长度控制，验证 why.md#需求:-最新日志 与 why.md#需求:-资源详情
- [√] 3.3 在 `internal/core/router.go` 中扩展 template card 事件处理：支持进入“容器查看”菜单与各查看动作，查看类输入后直接执行（无需二次确认），验证 why.md#需求:-资源概览
- [√] 3.4 在 `internal/core/*_test.go` 中补充测试：命令解析、状态机分支（查看类不走 confirm），验证点：流程不回退、输入非法时提示明确

## 4. 安全检查
- [√] 4.1 执行安全检查（按G9: 输入验证、敏感信息处理、权限控制、避免输出过长/注入风险）

## 5. 文档更新
- [√] 5.1 更新 `helloagents/wiki/modules/unraid.md`：补充“容器查看能力”规范与字段来源/限制说明
- [√] 5.2 更新 `helloagents/CHANGELOG.md`：记录新增“容器查看（状态/资源/日志）”能力

## 6. 测试
- [√] 6.1 运行 `go test ./...`，确保全部测试通过
