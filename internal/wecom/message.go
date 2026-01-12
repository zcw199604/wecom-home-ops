// Package wecom 封装企业微信自建应用的消息收发与交互卡片构建。
package wecom

type IncomingMessage struct {
	ToUserName   string `xml:"ToUserName"`
	FromUserName string `xml:"FromUserName"`
	MsgType      string `xml:"MsgType"`
	Content      string `xml:"Content"`
	Event        string `xml:"Event"`
	EventKey     string `xml:"EventKey"`
	TaskId       string `xml:"TaskId"`
	CardType     string `xml:"CardType"`
}

const (
	EventKeyUnraidMenuOps  = "unraid.menu.ops"
	EventKeyUnraidMenuView = "unraid.menu.view"

	EventKeyUnraidRestart     = "unraid.action.restart"
	EventKeyUnraidStop        = "unraid.action.stop"
	EventKeyUnraidForceUpdate = "unraid.action.force_update"

	EventKeyUnraidViewStatus      = "unraid.view.status"
	EventKeyUnraidViewStats       = "unraid.view.stats"
	EventKeyUnraidViewStatsDetail = "unraid.view.stats_detail"
	EventKeyUnraidViewLogs        = "unraid.view.logs"

	EventKeyUnraidBackToMenu = "unraid.menu.back"

	EventKeyConfirm = "core.action.confirm"
	EventKeyCancel  = "core.action.cancel"
)

type TextMessage struct {
	ToUser  string
	Content string
}

type TemplateCardMessage struct {
	ToUser string
	Card   TemplateCard
}

type TemplateCard map[string]interface{}

func NewUnraidEntryCard() TemplateCard {
	return TemplateCard{
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
}

func NewUnraidOpsCard() TemplateCard {
	return TemplateCard{
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
}

func NewUnraidViewCard() TemplateCard {
	return TemplateCard{
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
}

func NewConfirmCard(actionDisplayName, containerName string) TemplateCard {
	return TemplateCard{
		"card_type": "button_interaction",
		"main_title": map[string]interface{}{
			"title": "确认执行",
			"desc":  actionDisplayName + "：" + containerName,
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
}
