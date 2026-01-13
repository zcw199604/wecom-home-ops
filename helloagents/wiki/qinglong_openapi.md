# 青龙(QL) OpenAPI 调用方式（本项目整理）

本文档聚焦本项目实际使用到的青龙 OpenAPI 接口与调用约定，便于排障与二次开发。

---

## 1. 配置与前置条件

1. 在青龙面板「OpenAPI」中创建应用，获得：
   - `client_id`
   - `client_secret`
2. 在本项目 `config.yaml` 中配置 `qinglong.instances`：
   - `base_url`: 例如 `http://qinglong-host:5700`
   - `client_id` / `client_secret`

> 说明：本项目会对 `base_url` 做规范化（去掉末尾 `/`），并通过 `Authorization: Bearer <token>` 访问需要鉴权的 OpenAPI。

---

## 2. 通用返回结构（Envelope）

青龙 OpenAPI 常见响应结构为：

```json
{
  "code": 200,
  "data": {},
  "message": "",
  "errors": [{"message":"", "value":""}]
}
```

本项目判定失败的条件：
- HTTP 状态码非 2xx
- 或 `code != 200`（优先使用 `message` / `errors` 拼接错误信息）

---

## 3. 认证：获取并缓存 token

### 3.1 获取 token

**请求**
- Method: `GET`
- Path: `/open/auth/token`
- Query:
  - `client_id`
  - `client_secret`
  - `t`: 秒级时间戳（本项目会附带，减少缓存干扰）

**响应 data**
- `token`: access token
- `token_type`: 通常为 `Bearer`
- `expiration`: 秒级 Unix 时间戳

### 3.2 本项目缓存策略

实现位置：`internal/qinglong/client.go`
- 内存缓存 `token` 与 `expiration`
- 过期前 2 分钟视为不可用，触发刷新
- 使用 `singleflight` 合并并发刷新，避免过期瞬间的并发击穿

---

## 4. 本项目用到的 OpenAPI 接口

以下接口均需要 header：
- `Authorization: Bearer <token>`
- `Content-Type: application/json`

### 4.1 任务列表 / 搜索

**请求**
- Method: `GET`
- Path: `/open/crons`
- Query:
  - `searchValue`（可选）
  - `page`（可选，默认 1）
  - `size`（可选，默认 20）
  - `t`（秒级时间戳）

**响应 data**
```json
{
  "data": [
    {"id": 1, "name": "...", "command": "...", "schedule": "...", "isDisabled": 0, "status": 0}
  ],
  "total": 1
}
```

### 4.2 获取任务详情

**请求**
- Method: `GET`
- Path: `/open/crons/{id}`
- Query: `t`（秒级时间戳）

### 4.3 运行 / 启用 / 禁用任务

**请求**
- Method: `PUT`
- Path:
  - `/open/crons/run`
  - `/open/crons/enable`
  - `/open/crons/disable`
- Body: JSON 数组（任务 ID 列表），例如：
```json
[123, 456]
```

### 4.4 获取任务最近日志

**请求**
- Method: `GET`
- Path: `/open/crons/{id}/log`
- Query: `t`（秒级时间戳）

**响应 data**
- 字符串日志内容（本项目会做长度截断后回显到企业微信）

---

## 5. curl 调用示例（可用于排障）

> 注意：示例仅用于本地排障；不要把真实 token / secret 写入仓库或日志。

### 5.1 获取 token

```bash
curl -sG 'http://qinglong-host:5700/open/auth/token' \
  --data-urlencode 'client_id=YOUR_CLIENT_ID' \
  --data-urlencode 'client_secret=YOUR_CLIENT_SECRET' \
  --data-urlencode "t=$(date +%s)"
```

### 5.2 列表 / 搜索任务

```bash
curl -sG 'http://qinglong-host:5700/open/crons' \
  -H 'Authorization: Bearer YOUR_TOKEN' \
  --data-urlencode 'searchValue=jd' \
  --data-urlencode 'page=1' \
  --data-urlencode 'size=20' \
  --data-urlencode "t=$(date +%s)"
```

### 5.3 运行任务

```bash
curl -sX PUT 'http://qinglong-host:5700/open/crons/run' \
  -H 'Authorization: Bearer YOUR_TOKEN' \
  -H 'Content-Type: application/json' \
  -d '[123]'
```

### 5.4 获取任务日志

```bash
curl -sG 'http://qinglong-host:5700/open/crons/123/log' \
  -H 'Authorization: Bearer YOUR_TOKEN' \
  --data-urlencode "t=$(date +%s)"
```

