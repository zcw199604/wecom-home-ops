// Package wecom 封装企业微信自建应用的消息收发与交互卡片构建。
package wecom

// message.go 定义企业微信回调消息结构与交互卡片构造。
type IncomingMessage struct {
	ToUserName   string `xml:"ToUserName"`
	FromUserName string `xml:"FromUserName"`
	CreateTime   int64  `xml:"CreateTime"`
	MsgType      string `xml:"MsgType"`
	Content      string `xml:"Content"`
	Event        string `xml:"Event"`
	EventKey     string `xml:"EventKey"`
	MsgID        string `xml:"MsgId"`
	TaskId       string `xml:"TaskId"`
	CardType     string `xml:"CardType"`
	ResponseCode string `xml:"ResponseCode"`
}

const (
	EventKeyServiceSelectPrefix = "svc.select."

	// core.* 约定用于“应用自定义菜单（CLICK）”与通用命令的 EventKey。
	EventKeyCoreMenu     = "core.menu"
	EventKeyCoreHelp     = "core.help"
	EventKeyCoreSelfTest = "core.selftest"

	EventKeyUnraidMenuOps     = "unraid.menu.ops"
	EventKeyUnraidMenuView    = "unraid.menu.view"
	EventKeyUnraidBackToMenu  = "unraid.menu.back"
	EventKeyUnraidRestart     = "unraid.action.restart"
	EventKeyUnraidStop        = "unraid.action.stop"
	EventKeyUnraidForceUpdate = "unraid.action.force_update"
	EventKeyUnraidViewStatus  = "unraid.view.status"
	EventKeyUnraidViewStats   = "unraid.view.stats"

	EventKeyUnraidViewStatsDetail = "unraid.view.stats_detail"
	EventKeyUnraidViewLogs        = "unraid.view.logs"

	EventKeyQinglongMenu                 = "qinglong.menu"
	EventKeyQinglongInstanceSelectPrefix = "qinglong.instance.select."
	EventKeyQinglongActionList           = "qinglong.action.list"
	EventKeyQinglongActionSearch         = "qinglong.action.search"
	EventKeyQinglongActionByID           = "qinglong.action.by_id"
	EventKeyQinglongActionSwitchInstance = "qinglong.action.switch_instance"
	EventKeyQinglongCronSelectPrefix     = "qinglong.cron.select."
	EventKeyQinglongCronRun              = "qinglong.cron.run"
	EventKeyQinglongCronEnable           = "qinglong.cron.enable"
	EventKeyQinglongCronDisable          = "qinglong.cron.disable"
	EventKeyQinglongCronLog              = "qinglong.cron.log"

	EventKeyConfirm = "core.action.confirm"
	EventKeyCancel  = "core.action.cancel"
)

// Menu 定义企业微信“应用自定义菜单”的请求体。
//
// 官方文档（SSOT）：
// - 创建菜单：https://developer.work.weixin.qq.com/document/path/90231
// - 获取菜单：https://developer.work.weixin.qq.com/document/path/90232
// - 删除菜单：https://developer.work.weixin.qq.com/document/path/90233
type Menu struct {
	Buttons []MenuButton `json:"button"`
}

// MenuButton 定义菜单按钮（最多 3 个一级按钮，每个最多 5 个二级按钮）。
type MenuButton struct {
	Type string `json:"type,omitempty"`
	Name string `json:"name"`
	Key  string `json:"key,omitempty"`
	URL  string `json:"url,omitempty"`

	SubButtons []MenuButton `json:"sub_button,omitempty"`
}

// DefaultMenu 返回本项目推荐的“应用自定义菜单”。
// 该菜单以 CLICK 事件为主，EventKey 与回调路由约定保持一致。
func DefaultMenu() Menu {
	return Menu{
		Buttons: []MenuButton{
			{
				Name: "常用",
				SubButtons: []MenuButton{
					{Type: "click", Name: "操作菜单", Key: EventKeyCoreMenu},
					{Type: "click", Name: "自检", Key: EventKeyCoreSelfTest},
					{Type: "click", Name: "帮助", Key: EventKeyCoreHelp},
				},
			},
			{
				Name: "Unraid",
				SubButtons: []MenuButton{
					{Type: "click", Name: "进入菜单", Key: EventKeyServiceSelectPrefix + "unraid"},
					{Type: "click", Name: "容器操作", Key: EventKeyUnraidMenuOps},
					{Type: "click", Name: "容器查看", Key: EventKeyUnraidMenuView},
				},
			},
			{
				Name: "青龙",
				SubButtons: []MenuButton{
					{Type: "click", Name: "进入青龙", Key: EventKeyServiceSelectPrefix + "qinglong"},
					{Type: "click", Name: "动作菜单", Key: EventKeyQinglongMenu},
				},
			},
		},
	}
}

type TextMessage struct {
	ToUser  string
	Content string
}

type TemplateCardMessage struct {
	ToUser string
	Card   TemplateCard
}

type TemplateCard map[string]interface{}

const defaultCardSourceDesc = "wecom-home-ops"

func applyDefaultSource(card TemplateCard) TemplateCard {
	if card == nil {
		card = TemplateCard{}
	}
	if _, ok := card["source"]; ok {
		return card
	}
	card["source"] = map[string]interface{}{
		"desc":       defaultCardSourceDesc,
		"desc_color": 1,
	}
	return card
}

type ServiceOption struct {
	Key  string
	Name string
}

func NewServiceSelectCard(services []ServiceOption) TemplateCard {
	var buttons []map[string]interface{}
	for _, svc := range services {
		if svc.Key == "" || svc.Name == "" {
			continue
		}
		buttons = append(buttons, map[string]interface{}{
			"text":  svc.Name,
			"style": 1,
			"key":   EventKeyServiceSelectPrefix + svc.Key,
		})
	}

	card := TemplateCard{
		"card_type": "button_interaction",
		"main_title": map[string]interface{}{
			"title": "操作菜单",
			"desc":  "请选择服务",
		},
		"button_list": buttons,
	}
	return applyDefaultSource(card)
}

func NewUnraidEntryCard() TemplateCard {
	card := TemplateCard{
		"card_type": "button_interaction",
		"main_title": map[string]interface{}{
			"title": "Unraid 容器",
			"desc":  "请选择菜单",
		},
		"button_list": []map[string]interface{}{
			{
				"text":  "容器操作",
				"style": 1,
				"key":   EventKeyUnraidMenuOps,
			},
			{
				"text":  "容器查看",
				"style": 2,
				"key":   EventKeyUnraidMenuView,
			},
		},
	}
	return applyDefaultSource(card)
}

func NewUnraidOpsCard() TemplateCard {
	card := TemplateCard{
		"card_type": "button_interaction",
		"main_title": map[string]interface{}{
			"title": "Unraid 容器操作",
			"desc":  "请选择动作",
		},
		"button_list": []map[string]interface{}{
			{
				"text":  "重启容器",
				"style": 1,
				"key":   EventKeyUnraidRestart,
			},
			{
				"text":  "停止容器",
				"style": 2,
				"key":   EventKeyUnraidStop,
			},
			{
				"text":  "强制更新",
				"style": 2,
				"key":   EventKeyUnraidForceUpdate,
			},
			{
				"text":  "返回菜单",
				"style": 1,
				"key":   EventKeyUnraidBackToMenu,
			},
		},
	}
	return applyDefaultSource(card)
}

// NewUnraidActionCard 兼容旧命名：等价于 NewUnraidOpsCard。
func NewUnraidActionCard() TemplateCard { return NewUnraidOpsCard() }

func NewUnraidViewCard() TemplateCard {
	card := TemplateCard{
		"card_type": "button_interaction",
		"main_title": map[string]interface{}{
			"title": "Unraid 容器查看",
			"desc":  "请选择信息类型",
		},
		"button_list": []map[string]interface{}{
			{
				"text":  "查看状态",
				"style": 1,
				"key":   EventKeyUnraidViewStatus,
			},
			{
				"text":  "资源概览",
				"style": 1,
				"key":   EventKeyUnraidViewStats,
			},
			{
				"text":  "资源详情",
				"style": 2,
				"key":   EventKeyUnraidViewStatsDetail,
			},
			{
				"text":  "查看日志",
				"style": 2,
				"key":   EventKeyUnraidViewLogs,
			},
			{
				"text":  "返回菜单",
				"style": 1,
				"key":   EventKeyUnraidBackToMenu,
			},
		},
	}
	return applyDefaultSource(card)
}

type QinglongInstanceOption struct {
	ID   string
	Name string
}

func NewQinglongInstanceSelectCard(instances []QinglongInstanceOption) TemplateCard {
	var buttons []map[string]interface{}
	for _, ins := range instances {
		if ins.ID == "" || ins.Name == "" {
			continue
		}
		buttons = append(buttons, map[string]interface{}{
			"text":  ins.Name,
			"style": 1,
			"key":   EventKeyQinglongInstanceSelectPrefix + ins.ID,
		})
	}

	card := TemplateCard{
		"card_type": "button_interaction",
		"main_title": map[string]interface{}{
			"title": "青龙(QL)",
			"desc":  "请选择实例",
		},
		"button_list": buttons,
	}
	return applyDefaultSource(card)
}

func NewQinglongActionCard(instanceName string) TemplateCard {
	desc := "请选择动作"
	if instanceName != "" {
		desc = "实例：" + instanceName
	}
	card := TemplateCard{
		"card_type": "button_interaction",
		"main_title": map[string]interface{}{
			"title": "青龙(QL) 任务管理",
			"desc":  desc,
		},
		"button_list": []map[string]interface{}{
			{
				"text":  "任务列表",
				"style": 1,
				"key":   EventKeyQinglongActionList,
			},
			{
				"text":  "搜索任务",
				"style": 1,
				"key":   EventKeyQinglongActionSearch,
			},
			{
				"text":  "按ID操作",
				"style": 2,
				"key":   EventKeyQinglongActionByID,
			},
			{
				"text":  "切换实例",
				"style": 2,
				"key":   EventKeyQinglongActionSwitchInstance,
			},
		},
	}
	return applyDefaultSource(card)
}

type QinglongCronOption struct {
	ID   int
	Name string
}

func NewQinglongCronListCard(title, instanceName string, crons []QinglongCronOption) TemplateCard {
	desc := "请选择任务"
	if instanceName != "" {
		desc = "实例：" + instanceName
	}
	var buttons []map[string]interface{}
	for _, c := range crons {
		if c.ID <= 0 {
			continue
		}
		text := c.Name
		if text == "" {
			text = "任务"
		}
		buttons = append(buttons, map[string]interface{}{
			"text":  text,
			"style": 1,
			"key":   EventKeyQinglongCronSelectPrefix + intToString(c.ID),
		})
	}

	buttons = append(buttons, map[string]interface{}{
		"text":  "动作菜单",
		"style": 2,
		"key":   EventKeyQinglongMenu,
	})

	card := TemplateCard{
		"card_type": "button_interaction",
		"main_title": map[string]interface{}{
			"title": title,
			"desc":  desc,
		},
		"button_list": buttons,
	}
	return applyDefaultSource(card)
}

func NewQinglongCronActionCard(instanceName string, cronID int, cronName string) TemplateCard {
	desc := "请选择动作"
	if cronName != "" {
		desc = cronName
	}
	if instanceName != "" {
		desc = "实例：" + instanceName + " | " + desc
	}

	title := "任务操作"
	if cronID > 0 {
		title = "任务操作 - ID " + intToString(cronID)
	}

	card := TemplateCard{
		"card_type": "button_interaction",
		"main_title": map[string]interface{}{
			"title": title,
			"desc":  desc,
		},
		"button_list": []map[string]interface{}{
			{
				"text":  "运行",
				"style": 1,
				"key":   EventKeyQinglongCronRun,
			},
			{
				"text":  "启用",
				"style": 2,
				"key":   EventKeyQinglongCronEnable,
			},
			{
				"text":  "禁用",
				"style": 2,
				"key":   EventKeyQinglongCronDisable,
			},
			{
				"text":  "查看日志",
				"style": 1,
				"key":   EventKeyQinglongCronLog,
			},
			{
				"text":  "返回",
				"style": 2,
				"key":   EventKeyQinglongMenu,
			},
		},
	}
	return applyDefaultSource(card)
}

func NewConfirmCard(actionDisplayName, target string) TemplateCard {
	card := TemplateCard{
		"card_type": "button_interaction",
		"main_title": map[string]interface{}{
			"title": "确认执行",
			"desc":  actionDisplayName + "：" + target,
		},
		"button_list": []map[string]interface{}{
			{
				"text":  "确认",
				"style": 2,
				"key":   EventKeyConfirm,
			},
			{
				"text":  "取消",
				"style": 1,
				"key":   EventKeyCancel,
			},
		},
	}
	return applyDefaultSource(card)
}

func intToString(v int) string {
	if v == 0 {
		return "0"
	}
	var b [32]byte
	i := len(b)
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		i--
		b[i] = byte('0' + (v % 10))
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
