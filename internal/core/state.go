// Package core 提供企业微信会话的动作路由与状态管理。
package core

import (
	"sync"
	"time"

	"daily-help/internal/wecom"
)

type Step string

const (
	StepAwaitingContainerName Step = "awaiting_container_name"
	StepAwaitingConfirm       Step = "awaiting_confirm"
)

type Action string

const (
	ActionRestart     Action = "restart"
	ActionStop        Action = "stop"
	ActionForceUpdate Action = "force_update"

	ActionViewStatus      Action = "view_status"
	ActionViewStats       Action = "view_stats"
	ActionViewStatsDetail Action = "view_stats_detail"
	ActionViewLogs        Action = "view_logs"
)

func ActionFromEventKey(key string) Action {
	switch key {
	case wecom.EventKeyUnraidRestart:
		return ActionRestart
	case wecom.EventKeyUnraidStop:
		return ActionStop
	case wecom.EventKeyUnraidForceUpdate:
		return ActionForceUpdate
	case wecom.EventKeyUnraidViewStatus:
		return ActionViewStatus
	case wecom.EventKeyUnraidViewStats:
		return ActionViewStats
	case wecom.EventKeyUnraidViewStatsDetail:
		return ActionViewStatsDetail
	case wecom.EventKeyUnraidViewLogs:
		return ActionViewLogs
	default:
		return ""
	}
}

func (a Action) DisplayName() string {
	switch a {
	case ActionRestart:
		return "重启"
	case ActionStop:
		return "停止"
	case ActionForceUpdate:
		return "强制更新"
	case ActionViewStatus:
		return "查看状态"
	case ActionViewStats:
		return "资源概览"
	case ActionViewStatsDetail:
		return "资源详情"
	case ActionViewLogs:
		return "查看日志"
	default:
		return "未知动作"
	}
}

func (a Action) RequiresConfirm() bool {
	switch a {
	case ActionRestart, ActionStop, ActionForceUpdate:
		return true
	default:
		return false
	}
}

type ConversationState struct {
	Step          Step
	Action        Action
	ContainerName string
	ExpiresAt     time.Time
}

type StateStore struct {
	ttl  time.Duration
	mu   sync.Mutex
	data map[string]ConversationState
}

func NewStateStore(ttl time.Duration) *StateStore {
	return &StateStore{
		ttl:  ttl,
		data: make(map[string]ConversationState),
	}
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
