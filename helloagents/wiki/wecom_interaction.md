# 企业微信自建应用会话交互（速查）

> 面向本项目开发者：覆盖“回调接入（GET/POST）→ 收消息/事件 → 发送应用消息 → 模板卡片按钮回调”的最小闭环，并映射到仓库实现。

## 官方文档索引（SSOT）

- 回调配置（URL/Token/EncodingAESKey、GET 验证、POST 回调、响应时限与重试语义）：https://developer.work.weixin.qq.com/document/path/90930
- 接收消息与事件（总览）：https://developer.work.weixin.qq.com/document/path/90237
- 消息格式（回调入站消息明文结构）：https://developer.work.weixin.qq.com/document/path/90239
- 事件格式（含 enter_agent、template_card_event、template_card_menu_event）：https://developer.work.weixin.qq.com/document/path/90240
- 被动回复消息格式（仅在需要被动回复时使用；注意 To/From 方向与回调入站不同）：https://developer.work.weixin.qq.com/document/path/90241
- 发送应用消息（message/send、模板卡片类型与字段约束）：https://developer.work.weixin.qq.com/document/path/90236
- 更新模版卡片消息（update_template_card、消费 ResponseCode）：https://developer.work.weixin.qq.com/document/90000/90135/94888
- 获取 access_token（gettoken）：https://developer.work.weixin.qq.com/document/path/91039
- 应用自定义菜单：创建菜单（menu/create）：https://developer.work.weixin.qq.com/document/path/90231
- 应用自定义菜单：获取菜单（menu/get）：https://developer.work.weixin.qq.com/document/path/90232
- 应用自定义菜单：删除菜单（menu/delete）：https://developer.work.weixin.qq.com/document/path/90233

## 交互链路总览

1. 管理端配置“接收消息服务器”：`URL` + `Token` + `EncodingAESKey`
2. 保存配置时企业微信发起 **GET** 验证：`msg_signature/timestamp/nonce/echostr`
3. 运行期企业微信发起 **POST** 回调：`msg_signature/timestamp/nonce` + XML（含 `Encrypt`）
4. 服务端：校验签名 → 解密 → 解析明文消息/事件 → 业务处理（建议异步）→ 返回 `success`
5. 服务端调用企业微信 **发送应用消息**：文本/模板卡片
6. 用户点击模板卡片按钮：企业微信回调 **event=template_card_event**，携带 `EventKey/TaskId/CardType/ResponseCode`

## 回调接入（GET/POST）

### 回调配置项

依据官方“回调配置”文档（https://developer.work.weixin.qq.com/document/path/90930），回调服务需要三项配置：

- `URL`：回调服务地址（可被企业微信访问）
- `Token`：用于计算签名的自定义字符串（≤32 位）
- `EncodingAESKey`：用于消息体加解密（回调安全模式）

本项目还需要以下应用基础信息（用于发消息/鉴权）：

- `CorpID`：企业 ID
- `AgentID`：自建应用 ID
- `Secret`：自建应用 Secret

### URL 验证（GET）

官方说明（https://developer.work.weixin.qq.com/document/path/90930）：管理员保存回调配置时，企业微信会对回调 URL 发起 GET 验证请求，例如：

```text
GET https://<your-host>/wecom/callback?msg_signature=ASDF...&timestamp=13500001234&nonce=123412323&echostr=ENCRYPT_STR
```

参数语义（同上链接）：

- `msg_signature`：签名（由 token + timestamp + nonce + echostr 计算）
- `timestamp` / `nonce`：防重放
- `echostr`：加密字符串（解密后包含 `random/msg_len/msg/receiveid`，其中 `msg` 为明文）

处理要点（同上链接）：

1. 参数需 URLDecode
2. 校验签名：使用 `token + timestamp + nonce + echostr` 重新计算并比对 `msg_signature`
3. 解密 `echostr` 得到明文 `msg`
4. **1 秒内**响应：原样返回明文 `msg`（不能加引号、不能带 BOM、不能带换行）

### 业务回调（POST）

官方说明（https://developer.work.weixin.qq.com/document/path/90930）：运行期触发回调时，企业微信会发起 POST 请求，例如：

```text
POST https://<your-host>/wecom/callback?msg_signature=ASDF...&timestamp=13500001234&nonce=123412323
```

接收数据（同上链接，示意）：

```xml
<xml>
  <ToUserName><![CDATA[CORPID]]></ToUserName>
  <AgentID><![CDATA[1000002]]></AgentID>
  <Encrypt><![CDATA[ENCRYPT_STR]]></Encrypt>
</xml>
```

处理要点（同上链接）：

1. 校验签名：使用 `token + timestamp + nonce + Encrypt` 重新计算并比对 `msg_signature`
2. 解密 `Encrypt` 得到明文消息体（XML）
3. 业务处理后正确响应

重试语义（同上链接）：

- 企业微信服务器在 **5 秒内**收不到响应会断开并重新发起请求，总共重试三次
- 仅针对网络连接失败/请求超时情况重试，且无法保证 100% 回调成功
- 建议：收到回调后**立即应答**，业务**异步处理**，不要强依赖回调

### 签名与解密要点（实现核对）

结合官方“回调配置”文档中对签名与 `echostr` 解密字段的描述（https://developer.work.weixin.qq.com/document/path/90930）：

- `msg_signature`：SHA1 签名（由 `token/timestamp/nonce/(echostr|Encrypt)` 参与计算）
- 解密后的结构包含 `random/msg_len/msg/receiveid`，其中 `receiveid` 对自建应用通常为 `CorpID`

## 自建应用会话交互：常用消息/事件

### 文本消息（MsgType=text）

用于“关键词触发/参数输入”等场景（字段定义见：https://developer.work.weixin.qq.com/document/path/90239）。

示例明文 XML（示意）：

```xml
<xml>
  <ToUserName><![CDATA[CORPID]]></ToUserName>
  <FromUserName><![CDATA[USERID]]></FromUserName>
  <CreateTime>1700000000</CreateTime>
  <MsgType><![CDATA[text]]></MsgType>
  <Content><![CDATA[菜单]]></Content>
  <MsgId>12345</MsgId>
  <AgentID>1000002</AgentID>
</xml>
```

### 进入应用（Event=enter_agent）

官方说明（https://developer.work.weixin.qq.com/document/path/90240）：成员进入企业微信应用时触发。

示例明文 XML（示意）：

```xml
<xml>
  <ToUserName><![CDATA[CORPID]]></ToUserName>
  <FromUserName><![CDATA[USERID]]></FromUserName>
  <CreateTime>1700000000</CreateTime>
  <MsgType><![CDATA[event]]></MsgType>
  <Event><![CDATA[enter_agent]]></Event>
  <EventKey><![CDATA[]]></EventKey>
  <AgentID>1000002</AgentID>
</xml>
```

### 模板卡片按钮点击（Event=template_card_event）

官方说明（https://developer.work.weixin.qq.com/document/path/90240）：点击通用模板卡片按钮触发回调事件，关键字段包括：

- `EventKey`：与发送模板卡片时指定的按钮 `button_list.key` 相同
- `TaskId`：与发送模板卡片时指定的 `task_id` 相同
- `CardType`：模板卡片类型（如 `text_notice/news_notice/button_interaction/vote_interaction/multiple_interaction`）
- `ResponseCode`：用于调用**更新卡片接口**的凭据（官方说明：72 小时内有效且只能使用一次）

示例明文 XML（示意）：

```xml
<xml>
  <ToUserName><![CDATA[CORPID]]></ToUserName>
  <FromUserName><![CDATA[USERID]]></FromUserName>
  <CreateTime>1700000000</CreateTime>
  <MsgType><![CDATA[event]]></MsgType>
  <Event><![CDATA[template_card_event]]></Event>
  <EventKey><![CDATA[unraid.action.restart]]></EventKey>
  <TaskId><![CDATA[wecom-home-ops-1700000000000000000]]></TaskId>
  <CardType><![CDATA[button_interaction]]></CardType>
  <ResponseCode><![CDATA[RESPONSE_CODE]]></ResponseCode>
  <AgentID>1000002</AgentID>
</xml>
```

> 说明：本项目已支持消费 `ResponseCode` 并调用 `update_template_card` 将按钮更新为不可点击状态（避免重复点击；与 `TaskId` 去重策略一致）。

### 模板卡片右上角菜单（Event=template_card_menu_event）

官方说明（https://developer.work.weixin.qq.com/document/path/90240）：点击模板卡片右上角菜单按钮触发回调事件，字段同样包含 `EventKey/TaskId/CardType/ResponseCode`。

本项目当前未使用该能力，但可用于“更多操作/跳转/取消”等扩展交互。

## 发送应用消息（文本 / 模板卡片）

### 获取 access_token

官方说明（https://developer.work.weixin.qq.com/document/path/91039）：

```text
GET https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=ID&corpsecret=SECRET
```

安全提示（同上链接）：请勿将 `access_token` 返回给前端，应由后端缓存并代表前端调用企业微信 API。

### 发送应用消息（message/send）

官方说明（https://developer.work.weixin.qq.com/document/path/90236）：

```text
POST https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token=ACCESS_TOKEN
```

### 模板卡片：按钮交互型（button_interaction）

官方“发送应用消息”文档对按钮交互型卡片字段约束（https://developer.work.weixin.qq.com/document/path/90236）：

- `msgtype` 固定为 `template_card`
- `card_type` 填写 `button_interaction`
- `task_id`：同一应用内不可重复；仅允许数字、字母与 `_-@`；最长 128 字节
- `button_list`：最多 6 个按钮
  - `button_list.type`：`0`/不填=回调事件；`1`=跳转 URL
  - `button_list.key`：当 `type=0` 时必填，回调事件会将其作为 `EventKey` 返回（最长 1024 字节）
  - `button_list.style`：1~4，不填默认 1

最小可用 JSON 示例（回调型按钮）：

```json
{
  "touser": "USERID",
  "msgtype": "template_card",
  "agentid": 1000002,
  "template_card": {
    "card_type": "button_interaction",
    "main_title": { "title": "操作菜单", "desc": "请选择动作" },
    "task_id": "wecom-home-ops-1700000000000000000",
    "button_list": [
      { "type": 0, "text": "重启容器", "style": 1, "key": "unraid.action.restart" },
      { "type": 0, "text": "取消", "style": 2, "key": "core.action.cancel" }
    ]
  }
}
```

## 更新模版卡片消息（update_template_card）

官方说明（https://developer.work.weixin.qq.com/document/90000/90135/94888）要点：

- 请求方式：`POST`（HTTPS）
- 请求地址：`https://qyapi.weixin.qq.com/cgi-bin/message/update_template_card?access_token=ACCESS_TOKEN`
- `response_code`：可通过**发消息接口**返回值或**回调事件**获取；**72 小时内有效且只能使用一次**
- 按钮置灰（按钮交互型/投票选择型/多项选择型）：使用 `button.replace_name` 替换按钮文案并更新为不可点击

可复制请求示例（将按钮更新为不可点击状态）：

```bash
curl -sS -X POST 'https://qyapi.weixin.qq.com/cgi-bin/message/update_template_card?access_token=ACCESS_TOKEN' \
  -H 'Content-Type: application/json' \
  -d '{
    "agentid": 1000002,
    "response_code": "RESPONSE_CODE",
    "button": { "replace_name": "已处理" }
  }'
```

成功返回示例（同上链接）：

```json
{ "errcode": 0, "errmsg": "ok" }
```

## 与本项目实现的映射

### 配置项（config.yaml）

示例见 `config.example.yaml`：

- `wecom.corpid` / `wecom.agentid` / `wecom.secret`
- `wecom.token` / `wecom.encoding_aes_key`
- `wecom.api_base_url`（默认 `https://qyapi.weixin.qq.com/cgi-bin`）

回调/发消息字段与配置的对应关系（便于核对企业微信后台配置）：

- 回调入站：`<ToUserName>` = `wecom.corpid`，`<AgentID>` = `wecom.agentid`
- 回调入站：`<FromUserName>` = 成员 `UserID`（本项目用作会话 `wecom_userid`），需在 `auth.allowed_userids` 白名单内
- 发消息：`message/send` 的 `agentid` = `wecom.agentid`

### 路由入口

- `GET /wecom/callback`：回调 URL 验证
- `POST /wecom/callback`：接收消息/事件回调

装配位置：`internal/app/server.go`

### 回调处理链路

- 签名校验/解密/去重：`internal/wecom/callback.go`
  - 默认请求体上限 1MiB（`http.MaxBytesReader`）
  - 去重 key 优先级：`TaskId` → `MsgId` → 明文 `sha256`（吸收企业微信重试）
- 加解密与签名：`internal/wecom/crypto.go`

### 会话交互与状态机

- 路由：`internal/core/router.go`
  - `MsgType=text`：关键词触发（如“菜单”）+ 参数输入
  - `MsgType=event`：处理 `enter_agent` / `template_card_event`（卡片按钮回调）/ `CLICK`（应用自定义菜单）
  - `Step=awaiting_confirm`：支持文本“确认/取消”作为兜底（避免卡片不展示时无法继续）
- 状态：`internal/core/state.go`（step/action/参数/TTL）

### 收发自检（自动回复）

- 白名单用户发送 `ping`/`/ping` 或 `自检`（或点击应用底部菜单“自检”），服务会回复 `pong` 并附带 `server_time/msg_id` 等诊断字段，用于快速验证“回调接收 + 发消息 API”链路是否正常。
- 代码位置：`internal/core/router.go`

### 模板卡片构建与事件 key 约定

- 卡片结构与按钮 key：`internal/wecom/message.go`
  - `svc.select.<serviceKey>`：服务选择
  - `unraid.*`：Unraid 菜单与动作
  - `qinglong.*`：青龙菜单与动作
  - `core.action.confirm` / `core.action.cancel`：二次确认/取消
  - `core.menu` / `core.help` / `core.selftest`：通用命令与应用自定义菜单（CLICK）的 EventKey

### 发消息（文本/模板卡片）

- `internal/wecom/client.go`
  - `gettoken`：本地缓存 + singleflight 合并刷新
  - `message/send`：发送 text/template_card
  - `message/update_template_card`：消费 `ResponseCode` 更新卡片按钮为不可点击状态
  - `menu/create`：同步应用自定义菜单（默认菜单见 `wecom.DefaultMenu()`）
  - `task_id`：若未提供则自动生成 `wecom-home-ops-<unixnano>`（用于回调关联）

## 排障清单（高频问题）

1. URL 验证失败（GET）
   - 检查是否 **1 秒内**返回明文 msg（无引号、无 BOM、无换行）
   - 检查签名计算输入是否为 `token + timestamp + nonce + echostr`
2. POST 回调频繁重试/重复触发
   - 官方语义：5 秒无响应会重试 3 次（仅网络失败/超时）
   - 建议：回调 handler 立即返回 `success`，业务异步处理；同时做去重（本项目已内置）
3. 解密失败 / receiver_id 不匹配
   - 检查 `EncodingAESKey` 与 `CorpID`（receiver_id）是否对应当前自建应用
4. 模板卡片按钮点了没反应
   - 确认按钮 `type=0` 且设置 `key`
   - 确认回调事件为 `template_card_event` 且能在回调明文中看到 `EventKey/TaskId`
5. 发送消息报错（errcode/errmsg）
   - 检查 `agentid`、`secret`、接收者 `touser` 是否正确，以及 `access_token` 是否过期
