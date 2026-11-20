// Package bandwidth 对文件传输实施按 IP 和全局共享的双层带宽限制。
package bandwidth

import (
	"context"
	"math"
	"net/netip"
	"sync"
	"time"

	"Canyou/internal/config"
)

const (
	chunkSize = 32 * 1024
	maxBurst  = 64 * 1024
	idleTTL   = 5 * time.Minute
)

type Direction uint8

const (
	Upload Direction = iota
	Download
)

// clock 使配额算法可用可控时钟验证，生产环境采用单调时间。
type clock interface {
	Now() time.Time
	Timer(time.Duration) (<-chan time.Time, func())
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) Timer(d time.Duration) (<-chan time.Time, func()) {
	timer := time.NewTimer(d)
	return timer.C, func() { timer.Stop() }
}

type bucket struct {
	rate       int64
	tokens     float64
	last       time.Time
	generation uint64
}

func capacity(rate int64) int {
	if rate == 0 {
		return 0
	}
	return int(min(int64(maxBurst), max(int64(1), rate/10)))
}

func (b *bucket) refill(now time.Time) {
	if now.After(b.last) {
		b.tokens = math.Min(float64(capacity(b.rate)), b.tokens+now.Sub(b.last).Seconds()*float64(b.rate))
		b.last = now
	}
}

func (b *bucket) setRate(rate int64, now time.Time) {
	if b.rate == rate {
		return
	}
	b.refill(now)
	b.rate = rate
	b.tokens = math.Min(b.tokens, float64(capacity(rate)))
	b.generation++
}

type peer struct {
	bucket   bucket
	active   int
	lastUsed time.Time
}

type directionState struct {
	mu        sync.Mutex
	clock     clock
	perIP     int64
	total     bucket
	peers     map[string]*peer
	changed   chan struct{}
	lastSweep time.Time
}

// Manager 必须由同一个服务的所有文件处理器共享，不能按请求新建。
type Manager struct {
	updateMu sync.Mutex
	states   [2]*directionState
}

func New() *Manager { return newManager(realClock{}) }

func newManager(c clock) *Manager {
	manager := &Manager{}
	for i := range manager.states {
		manager.states[i] = &directionState{clock: c, total: bucket{last: c.Now()}, peers: make(map[string]*peer), changed: make(chan struct{})}
	}
	return manager
}

func (manager *Manager) Update(limits config.BandwidthLimits) error {
	if err := limits.Validate(); err != nil {
		return err
	}
	manager.updateMu.Lock()
	defer manager.updateMu.Unlock()
	manager.states[Upload].update(limits.PerIPUpload, limits.TotalUpload)
	manager.states[Download].update(limits.PerIPDownload, limits.TotalDownload)
	return nil
}

func (state *directionState) notify() {
	close(state.changed)
	state.changed = make(chan struct{})
}

func (state *directionState) update(perIP, total int64) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.perIP == perIP && state.total.rate == total {
		return
	}
	now := state.clock.Now()
	state.total.setRate(total, now)
	state.perIP = perIP
	for _, peer := range state.peers {
		peer.bucket.setRate(perIP, now)
	}
	state.notify()
}

// peerKey 不接受任何代理头；不同端口和 IPv4 映射形式不能获取新额度。
func peerKey(remoteAddr string) string {
	if endpoint, err := netip.ParseAddrPort(remoteAddr); err == nil {
		return endpoint.Addr().Unmap().String()
	}
	if addr, err := netip.ParseAddr(remoteAddr); err == nil {
		return addr.Unmap().String()
	}
	return "unknown"
}

// Transfer 在整个请求期间固定 IP 条目，防止清理与长时间传输相互干扰。
type Transfer struct {
	state *directionState
	peer  *peer
	ctx   context.Context
	once  sync.Once
}

func (manager *Manager) Open(ctx context.Context, remoteAddr string, direction Direction) *Transfer {
	state := manager.states[direction]
	state.mu.Lock()
	defer state.mu.Unlock()
	now := state.clock.Now()
	if now.Sub(state.lastSweep) >= time.Minute {
		for key, entry := range state.peers {
			if entry.active == 0 && now.Sub(entry.lastUsed) > idleTTL {
				delete(state.peers, key)
			}
		}
		state.lastSweep = now
	}
	key := peerKey(remoteAddr)
	entry := state.peers[key]
	if entry == nil {
		entry = &peer{bucket: bucket{rate: state.perIP, tokens: float64(capacity(state.perIP)), last: now}}
		state.peers[key] = entry
	}
	entry.active++
	entry.lastUsed = now
	return &Transfer{state: state, peer: entry, ctx: ctx}
}

func (transfer *Transfer) Close() {
	transfer.once.Do(func() {
		transfer.state.mu.Lock()
		defer transfer.state.mu.Unlock()
		transfer.peer.active--
		transfer.peer.lastUsed = transfer.state.clock.Now()
	})
}

type ticket struct {
	n               int
	peerGeneration  uint64
	totalGeneration uint64
}

// take 同时检查两层；不足时不扣任何一层，也不预约未来配额。
func (transfer *Transfer) take(want int) (ticket, time.Duration, <-chan struct{}) {
	state := transfer.state
	state.mu.Lock()
	defer state.mu.Unlock()
	now := state.clock.Now()
	n := min(want, chunkSize)
	buckets := []*bucket{&transfer.peer.bucket, &state.total}
	for _, b := range buckets {
		b.refill(now)
		if b.rate != 0 {
			n = min(n, capacity(b.rate))
		}
	}
	var wait time.Duration
	for _, b := range buckets {
		if b.rate != 0 && b.tokens < float64(n) {
			delay := time.Duration(math.Ceil((float64(n) - b.tokens) / float64(b.rate) * float64(time.Second)))
			wait = max(wait, delay)
		}
	}
	if wait > 0 {
		return ticket{}, wait, state.changed
	}
	for _, b := range buckets {
		if b.rate != 0 {
			b.tokens -= float64(n)
		}
	}
	return ticket{n, transfer.peer.bucket.generation, state.total.generation}, 0, state.changed
}

func (transfer *Transfer) acquire(want int) (ticket, error) {
	for {
		if err := transfer.ctx.Err(); err != nil {
			return ticket{}, err
		}
		grant, delay, changed := transfer.take(want)
		if delay == 0 {
			return grant, nil
		}
		elapsed, stop := transfer.state.clock.Timer(delay)
		select {
		case <-transfer.ctx.Done():
			stop()
			return ticket{}, transfer.ctx.Err()
		case <-changed:
		case <-elapsed:
		}
		stop()
	}
}

func (transfer *Transfer) refund(grant ticket, unused int) {
	if unused <= 0 {
		return
	}
	state := transfer.state
	state.mu.Lock()
	defer state.mu.Unlock()
	now := state.clock.Now()
	for _, entry := range []struct {
		b          *bucket
		generation uint64
	}{
		{&transfer.peer.bucket, grant.peerGeneration}, {&state.total, grant.totalGeneration},
	} {
		// 参数变更之前的配额不能补入新桶，避免反复热更新凭空增加额度。
		if entry.b.rate != 0 && entry.b.generation == entry.generation {
			entry.b.refill(now)
			entry.b.tokens = math.Min(float64(capacity(entry.b.rate)), entry.b.tokens+float64(unused))
		}
	}
	state.notify()
}
