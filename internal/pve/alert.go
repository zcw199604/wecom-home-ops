package pve

// alert.go 实现 PVE 指标轮询告警（CPU/内存/存储阈值），并提供静默/冷却能力。
import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zcw199604/wecom-home-ops/internal/core"
	"github.com/zcw199604/wecom-home-ops/internal/wecom"
)

type AlertConfig struct {
	Enabled bool

	Interval time.Duration
	Cooldown time.Duration
	MuteFor  time.Duration

	CPUUsageThreshold     float64
	MemUsageThreshold     float64
	StorageUsageThreshold float64
}

type AlertManagerDeps struct {
	WeCom     core.WeComSender
	UserIDs   []string
	Instances []Instance
	Config    AlertConfig
}

type AlertManager struct {
	wecom   core.WeComSender
	userIDs []string

	cfg       AlertConfig
	instances map[string]Instance
	order     []Instance

	mu        sync.Mutex
	muteUntil map[string]time.Time
	lastSent  map[string]time.Time

	stopCh   chan struct{}
	stopOnce sync.Once

	startOnce sync.Once
}

func NewAlertManager(deps AlertManagerDeps) *AlertManager {
	instances := make(map[string]Instance)
	var order []Instance
	for _, ins := range deps.Instances {
		if !isValidInstanceID(ins.ID) || strings.TrimSpace(ins.Name) == "" || ins.Client == nil {
			continue
		}
		if _, exists := instances[ins.ID]; exists {
			continue
		}
		instances[ins.ID] = ins
		order = append(order, ins)
	}

	userIDs := uniqueNonEmpty(deps.UserIDs)

	return &AlertManager{
		wecom:      deps.WeCom,
		userIDs:    userIDs,
		cfg:        deps.Config,
		instances:  instances,
		order:      order,
		muteUntil:  make(map[string]time.Time),
		lastSent:   make(map[string]time.Time),
		stopCh:     make(chan struct{}),
	}
}

func (m *AlertManager) Enabled() bool { return m != nil && m.cfg.Enabled }

func (m *AlertManager) Config() AlertConfig {
	if m == nil {
		return AlertConfig{}
	}
	return m.cfg
}

func (m *AlertManager) Start() {
	if m == nil || !m.cfg.Enabled || m.wecom == nil || len(m.userIDs) == 0 || len(m.order) == 0 {
		return
	}
	m.startOnce.Do(func() {
		interval := m.cfg.Interval
		if interval <= 0 {
			interval = 2 * time.Minute
		}
		go m.loop(interval)
	})
}

func (m *AlertManager) Close() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() { close(m.stopCh) })
}

func (m *AlertManager) Mute(instanceID string, until time.Time) bool {
	if m == nil {
		return false
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.muteUntil[instanceID] = until
	return true
}

func (m *AlertManager) Unmute(instanceID string) bool {
	if m == nil {
		return false
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.muteUntil, instanceID)
	return true
}

func (m *AlertManager) MuteUntil(instanceID string) (time.Time, bool) {
	if m == nil {
		return time.Time{}, false
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return time.Time{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.muteUntil[instanceID]
	return t, ok && time.Now().Before(t)
}

func (m *AlertManager) loop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 启动后先做一次检查，避免需要等待一个 interval。
	m.checkOnce()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.checkOnce()
		}
	}
}

func (m *AlertManager) checkOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, ins := range m.order {
		m.checkInstance(ctx, ins)
	}
}

type alertKind string

const (
	alertKindCPU     alertKind = "cpu"
	alertKindMem     alertKind = "mem"
	alertKindStorage alertKind = "storage"
)

func (m *AlertManager) checkInstance(ctx context.Context, ins Instance) {
	if ins.Client == nil {
		return
	}

	if _, ok := m.MuteUntil(ins.ID); ok {
		return
	}

	if m.cfg.CPUUsageThreshold > 0 {
		m.checkCPU(ctx, ins)
	}
	if m.cfg.MemUsageThreshold > 0 {
		m.checkMem(ctx, ins)
	}
	if m.cfg.StorageUsageThreshold > 0 {
		m.checkStorage(ctx, ins)
	}
}

func (m *AlertManager) checkCPU(ctx context.Context, ins Instance) {
	nodes, err := ins.Client.ListClusterResources(ctx, "node")
	if err != nil {
		return
	}

	type hit struct {
		Node string
		CPU  float64
	}
	var hits []hit
	for _, n := range nodes {
		if strings.TrimSpace(n.Node) == "" {
			continue
		}
		p := n.CPU * 100
		if p >= m.cfg.CPUUsageThreshold {
			hits = append(hits, hit{Node: n.Node, CPU: p})
		}
	}
	if len(hits) == 0 {
		return
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].CPU > hits[j].CPU })

	const maxLines = 6
	var lines []string
	for i, h := range hits {
		if i >= maxLines {
			break
		}
		lines = append(lines, fmt.Sprintf("- %s: %.0f%%", h.Node, h.CPU))
	}

	m.sendIfNotInCooldown(ctx, ins, alertKindCPU, fmt.Sprintf(
		"⚠️ PVE 告警（CPU ≥ %.0f%%）\n实例：%s\n\n%s\n\n提示：如需静默请进入“菜单 → PVE → 静默告警”（不要回复序号）。",
		m.cfg.CPUUsageThreshold,
		ins.Name,
		strings.Join(lines, "\n"),
	))
}

func (m *AlertManager) checkMem(ctx context.Context, ins Instance) {
	nodes, err := ins.Client.ListClusterResources(ctx, "node")
	if err != nil {
		return
	}

	type hit struct {
		Node string
		Mem  float64
	}
	var hits []hit
	for _, n := range nodes {
		if strings.TrimSpace(n.Node) == "" || n.MaxMem <= 0 {
			continue
		}
		p := (float64(n.Mem) / float64(n.MaxMem)) * 100
		if p >= m.cfg.MemUsageThreshold {
			hits = append(hits, hit{Node: n.Node, Mem: p})
		}
	}
	if len(hits) == 0 {
		return
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Mem > hits[j].Mem })

	const maxLines = 6
	var lines []string
	for i, h := range hits {
		if i >= maxLines {
			break
		}
		lines = append(lines, fmt.Sprintf("- %s: %.0f%%", h.Node, h.Mem))
	}

	m.sendIfNotInCooldown(ctx, ins, alertKindMem, fmt.Sprintf(
		"⚠️ PVE 告警（内存 ≥ %.0f%%）\n实例：%s\n\n%s\n\n提示：如需静默请进入“菜单 → PVE → 静默告警”（不要回复序号）。",
		m.cfg.MemUsageThreshold,
		ins.Name,
		strings.Join(lines, "\n"),
	))
}

func (m *AlertManager) checkStorage(ctx context.Context, ins Instance) {
	storages, err := ins.Client.ListClusterResources(ctx, "storage")
	if err != nil {
		return
	}

	type hit struct {
		Node    string
		Storage string
		Usage   float64
	}
	var hits []hit
	for _, s := range storages {
		if strings.TrimSpace(s.Storage) == "" || s.MaxDisk <= 0 {
			continue
		}
		p := (float64(s.Disk) / float64(s.MaxDisk)) * 100
		if p >= m.cfg.StorageUsageThreshold {
			hits = append(hits, hit{Node: s.Node, Storage: s.Storage, Usage: p})
		}
	}
	if len(hits) == 0 {
		return
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Usage > hits[j].Usage })

	const maxLines = 8
	var lines []string
	for i, h := range hits {
		if i >= maxLines {
			break
		}
		name := h.Storage
		if strings.TrimSpace(h.Node) != "" {
			name = h.Node + "/" + h.Storage
		}
		lines = append(lines, fmt.Sprintf("- %s: %.0f%%", name, h.Usage))
	}

	m.sendIfNotInCooldown(ctx, ins, alertKindStorage, fmt.Sprintf(
		"⚠️ PVE 告警（存储 ≥ %.0f%%）\n实例：%s\n\n%s\n\n提示：如需静默请进入“菜单 → PVE → 静默告警”（不要回复序号）。",
		m.cfg.StorageUsageThreshold,
		ins.Name,
		strings.Join(lines, "\n"),
	))
}

func (m *AlertManager) sendIfNotInCooldown(ctx context.Context, ins Instance, kind alertKind, content string) {
	if m == nil || strings.TrimSpace(content) == "" {
		return
	}

	key := ins.ID + "|" + kind.String()
	now := time.Now()

	m.mu.Lock()
	last := m.lastSent[key]
	cooldown := m.cfg.Cooldown
	if cooldown <= 0 {
		cooldown = 10 * time.Minute
	}
	if !last.IsZero() && now.Sub(last) < cooldown {
		m.mu.Unlock()
		return
	}
	m.lastSent[key] = now
	m.mu.Unlock()

	for _, userID := range m.userIDs {
		_ = m.wecom.SendText(ctx, wecom.TextMessage{
			ToUser:  userID,
			Content: content,
		})
	}
}

func (k alertKind) String() string { return string(k) }

func uniqueNonEmpty(ss []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
