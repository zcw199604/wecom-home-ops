// Package core 提供企业微信会话路由、状态机与可插拔服务分发等核心能力。
package core

import (
	"sync"
	"time"

	"github.com/zcw199604/wecom-home-ops/internal/wecom"
)

type Step string

const (
	StepAwaitingContainerName         Step = "awaiting_container_name"
	StepAwaitingConfirm               Step = "awaiting_confirm"
	StepAwaitingQinglongSearchKeyword Step = "awaiting_qinglong_search_keyword"
	StepAwaitingQinglongCronID        Step = "awaiting_qinglong_cron_id"

	// StepAwaitingUnraidOpsAction 表示处于 Unraid “容器操作”菜单选择阶段（文本模式）。
	StepAwaitingUnraidOpsAction Step = "awaiting_unraid_ops_action"
	// StepAwaitingUnraidViewAction 表示处于 Unraid “容器查看”菜单选择阶段（文本模式）。
	StepAwaitingUnraidViewAction Step = "awaiting_unraid_view_action"
)

type Action string

const (
	ActionUnraidRestart     Action = "restart"
	ActionUnraidStop        Action = "stop"
	ActionUnraidForceUpdate Action = "force_update"

	ActionUnraidViewStatus      Action = "view_status"
	ActionUnraidViewStats       Action = "view_stats"
	ActionUnraidViewStatsDetail Action = "view_stats_detail"
	ActionUnraidViewLogs        Action = "view_logs"

	ActionQinglongRun     Action = "run"
	ActionQinglongEnable  Action = "enable"
	ActionQinglongDisable Action = "disable"
)

func ActionFromEventKey(key string) Action {
	switch key {
	case wecom.EventKeyUnraidRestart:
		return ActionUnraidRestart
	case wecom.EventKeyUnraidStop:
		return ActionUnraidStop
	case wecom.EventKeyUnraidForceUpdate:
		return ActionUnraidForceUpdate
	case wecom.EventKeyUnraidViewStatus:
		return ActionUnraidViewStatus
	case wecom.EventKeyUnraidViewStats:
		return ActionUnraidViewStats
	case wecom.EventKeyUnraidViewStatsDetail:
		return ActionUnraidViewStatsDetail
	case wecom.EventKeyUnraidViewLogs:
		return ActionUnraidViewLogs
	default:
		return ""
	}
}

func (a Action) DisplayName() string {
	switch a {
	case ActionUnraidRestart:
		return "重启"
	case ActionUnraidStop:
		return "停止"
	case ActionUnraidForceUpdate:
		return "强制更新"
	case ActionUnraidViewStatus:
		return "查看状态"
	case ActionUnraidViewStats:
		return "资源概览"
	case ActionUnraidViewStatsDetail:
		return "资源详情"
	case ActionUnraidViewLogs:
		return "查看日志"
	case ActionQinglongRun:
		return "运行"
	case ActionQinglongEnable:
		return "启用"
	case ActionQinglongDisable:
		return "禁用"
	default:
		return "未知动作"
	}
}

func (a Action) RequiresConfirm() bool {
	switch a {
	case ActionUnraidRestart, ActionUnraidStop, ActionUnraidForceUpdate,
		ActionQinglongRun, ActionQinglongEnable, ActionQinglongDisable:
		return true
	default:
		return false
	}
}

type ConversationState struct {
	Step       Step
	ServiceKey string
	InstanceID string

	Action        Action
	ContainerName string
	CronID        int

	// PendingButtons 用于模板卡片(button_interaction)的文本兜底：当用户回复“序号”时，映射到对应的 EventKey。
	PendingButtons []wecom.TemplateCardButton

	ExpiresAt time.Time
}

type StateStore struct {
	ttl  time.Duration
	mu   sync.Mutex
	data map[string]ConversationState

	stopCh   chan struct{}
	stopOnce sync.Once
}

func NewStateStore(ttl time.Duration) *StateStore {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}

	s := &StateStore{
		ttl:    ttl,
		data:   make(map[string]ConversationState),
		stopCh: make(chan struct{}),
	}
	s.startJanitor(minDuration(ttl, time.Minute))
	return s
}

func (s *StateStore) Get(userID string) (ConversationState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.data[userID]
	if !ok {
		return ConversationState{}, false
	}
	if time.Now().After(state.ExpiresAt) {
		delete(s.data, userID)
		return ConversationState{}, false
	}
	return state, true
}

func (s *StateStore) Set(userID string, state ConversationState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state.ExpiresAt = time.Now().Add(s.ttl)
	s.data[userID] = state
}

func (s *StateStore) Clear(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, userID)
}

func (s *StateStore) Close() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

func (s *StateStore) startJanitor(interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.pruneExpired()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *StateStore) pruneExpired() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.data {
		if now.After(v.ExpiresAt) {
			delete(s.data, k)
		}
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= b {
		return a
	}
	return b
}
