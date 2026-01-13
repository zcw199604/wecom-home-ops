# wecom-home-ops

企业微信自建应用回调 + 家庭本地服务统一中间层（MVP: Unraid/青龙）。

## 功能（MVP）
- 企业微信应用会话交互：服务选择 + 按钮选择动作 + 文本输入/列表选择参数 + 二次确认
- Unraid 容器操作：重启 / 停止 / 强制更新（通过 GraphQL introspection 探测 API 能力）
- 青龙(QL) 任务管理：任务列表/搜索 / 运行 / 启用 / 禁用 / 查看最近日志（OpenAPI，多实例）

## 快速开始
1. 复制配置并填写：
   - `cp config.example.yaml config.yaml`
2. 启动服务：
   - `go run ./cmd/wecom-home-ops -config config.yaml`
3. 在企业微信自建应用中配置“接收消息服务器”：
   - 回调 URL：`https://<你的域名>/wecom/callback`
   - Token / EncodingAESKey：与 `config.yaml` 对应

## 使用 Docker 启动
1. 构建镜像：
   - `docker build -t wecom-home-ops:local .`
2. 启动容器（默认读取 `/config/config.yaml`）：
   - `docker run -d --name wecom-home-ops --restart unless-stopped -p 8080:8080 -v "$(pwd)/config.yaml:/config/config.yaml:ro" wecom-home-ops:local`
3. 查看日志：
   - `docker logs -f wecom-home-ops`

> 注意：容器默认以 nonroot 运行，请确保 `config.yaml` 对其他用户可读（如 `chmod 644 config.yaml`）。如修改 `server.listen_addr` 端口，请同步调整 `-p` 映射。

## 使用说明（企业微信会话）
- 输入“菜单”打开服务选择
- 输入“容器/unraid”直达 Unraid 菜单
- 输入“青龙/ql”直达青龙菜单
- 输入“ping/自检”进行收发自检（自动回复 pong）

> 注意：企业微信回调通常要求公网可访问的 HTTPS 地址，可通过反向代理/内网穿透实现。
