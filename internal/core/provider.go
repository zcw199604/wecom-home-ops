package core

// provider.go 定义可插拔服务 Provider 的抽象接口。
import (
	"context"

	"github.com/zcw199604/wecom-home-ops/internal/wecom"
)

// WeComSender 抽象企业微信消息发送能力，便于在 core 与各 Provider 中复用与测试替身注入。
type WeComSender interface {
	SendText(ctx context.Context, msg wecom.TextMessage) error
	SendTemplateCard(ctx context.Context, msg wecom.TemplateCardMessage) error
}

// ServiceProvider 定义一个可插拔服务处理器，用于承载不同后端服务的交互与执行逻辑。
type ServiceProvider interface {
	Key() string
	DisplayName() string
	EntryKeywords() []string

	OnEnter(ctx context.Context, userID string) error

	HandleText(ctx context.Context, userID, content string) (bool, error)
	HandleEvent(ctx context.Context, userID string, msg wecom.IncomingMessage) (bool, error)
	HandleConfirm(ctx context.Context, userID string) (bool, error)
}
