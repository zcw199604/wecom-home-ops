package wecom

import (
	"sync"
	"time"
)

// Deduper 用于在短时间窗口内对“同一回调消息”做去重，吸收企业微信重试，避免重复执行业务逻辑。
// 注意：这是内存去重，服务重启后会丢失。
type Deduper struct {
	ttl time.Duration

	mu   sync.Mutex
	data map[string]time.Time

	stopCh   chan struct{}
	stopOnce sync.Once
}

func NewDeduper(ttl time.Duration) *Deduper {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	d := &Deduper{
		ttl:    ttl,
		data:   make(map[string]time.Time),
		stopCh: make(chan struct{}),
	}
	d.startJanitor(minDuration(ttl, time.Minute))
	return d
}

// SeenOrMark 如果 key 已存在且未过期，返回 true；否则写入并返回 false。
func (d *Deduper) SeenOrMark(key string) bool {
	if d == nil || key == "" {
		return false
	}
	now := time.Now()
	exp := now.Add(d.ttl)

	d.mu.Lock()
	defer d.mu.Unlock()

	if t, ok := d.data[key]; ok && now.Before(t) {
		return true
	}
	d.data[key] = exp
	return false
}

func (d *Deduper) Close() {
	if d == nil {
		return
	}
	d.stopOnce.Do(func() { close(d.stopCh) })
}

func (d *Deduper) startJanitor(interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				d.pruneExpired()
			case <-d.stopCh:
				return
			}
		}
	}()
}

func (d *Deduper) pruneExpired() {
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, v := range d.data {
		if now.After(v) {
			delete(d.data, k)
		}
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= b {
		return a
	}
	return b
}
