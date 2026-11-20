package bandwidth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"Canyou/internal/config"
)

type fakeTimer struct {
	at time.Time
	ch chan time.Time
}
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	timers  map[*fakeTimer]bool
	started chan struct{}
}

func newClock() *fakeClock {
	return &fakeClock{now: time.Unix(1700000000, 0), timers: make(map[*fakeTimer]bool), started: make(chan struct{}, 100)}
}
func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Timer(d time.Duration) (<-chan time.Time, func()) {
	c.mu.Lock()
	timer := &fakeTimer{c.now.Add(d), make(chan time.Time, 1)}
	c.timers[timer] = true
	c.mu.Unlock()
	c.started <- struct{}{}
	return timer.ch, func() { c.mu.Lock(); delete(c.timers, timer); c.mu.Unlock() }
}
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	for timer := range c.timers {
		if !timer.at.After(c.now) {
			timer.ch <- c.now
			delete(c.timers, timer)
		}
	}
}
func waitTimer(t *testing.T, c *fakeClock) {
	t.Helper()
	select {
	case <-c.started:
	case <-time.After(time.Second):
		t.Fatal("没有进入配额等待")
	}
}
func update(t *testing.T, manager *Manager, limits config.BandwidthLimits) {
	t.Helper()
	if err := manager.Update(limits); err != nil {
		t.Fatal(err)
	}
}
func open(t *testing.T, m *Manager, ip string, direction Direction) *Transfer {
	t.Helper()
	transfer := m.Open(context.Background(), ip, direction)
	t.Cleanup(transfer.Close)
	return transfer
}

func TestSharedIPAndDirections(t *testing.T) {
	c := newClock()
	m := newManager(c)
	update(t, m, config.BandwidthLimits{PerIPDownload: 1000})
	a := open(t, m, "192.0.2.1:123", Download)
	b := open(t, m, "[::ffff:192.0.2.1]:456", Download)
	if a.peer != b.peer {
		t.Fatal("同 IP 不同端口/映射地址获得独立额度")
	}
	if grant, delay, _ := a.take(100); grant.n != 100 || delay != 0 {
		t.Fatal(grant, delay)
	}
	if grant, delay, _ := b.take(100); grant.n != 0 || delay != 100*time.Millisecond {
		t.Fatal(grant, delay)
	}
	a.Close()
	b.Close()
	reconnected := open(t, m, "192.0.2.1:789", Download)
	if grant, _, _ := reconnected.take(100); grant.n != 0 {
		t.Fatal("重连重置了配额")
	}
	other := open(t, m, "192.0.2.2:123", Download)
	if grant, _, _ := other.take(100); grant.n != 100 {
		t.Fatal("不同 IP 互相占用独立额度")
	}
	up := open(t, m, "192.0.2.1:123", Upload)
	if grant, _, _ := up.take(999999); grant.n != chunkSize {
		t.Fatal("上下行串用配额或不限速块大小错误")
	}
	c.advance(100 * time.Millisecond)
	if grant, _, _ := reconnected.take(100); grant.n != 100 {
		t.Fatal("配额未恢复")
	}
	for _, pair := range [][2]string{{"[2001:db8::1]:2", "[2001:0db8:0:0:0:0:0:1]:3"}, {"invalid", "also-invalid"}} {
		if peerKey(pair[0]) != peerKey(pair[1]) {
			t.Fatal("地址未规范化", pair)
		}
	}
}

func TestTwoLayerAtomicBudget(t *testing.T) {
	c := newClock()
	m := newManager(c)
	update(t, m, config.BandwidthLimits{PerIPDownload: 1000, TotalDownload: 500})
	a, b := open(t, m, "192.0.2.1:1", Download), open(t, m, "192.0.2.2:1", Download)
	if grant, delay, _ := a.take(999999); grant.n != 0 || delay != 100*time.Millisecond {
		t.Fatal(grant, delay)
	}
	if a.peer.bucket.tokens != 100 {
		t.Fatal("总配额不足时扣了 IP 配额")
	}
	c.advance(100 * time.Millisecond)
	if grant, _, _ := a.take(999999); grant.n != 50 {
		t.Fatal(grant)
	}
	if grant, _, _ := b.take(50); grant.n != 0 || b.peer.bucket.tokens != 100 {
		t.Fatal("总桶未共享或提前扣了另一层")
	}
	c.advance(100 * time.Millisecond)
	if grant, _, _ := b.take(50); grant.n != 50 {
		t.Fatal(grant)
	}
}

func TestConcurrentAggregateBudgets(t *testing.T) {
	for _, limits := range []config.BandwidthLimits{
		{}, {PerIPDownload: 1000}, {TotalDownload: 1500}, {PerIPDownload: 1000, TotalDownload: 1500},
	} {
		c := newClock()
		m := newManager(c)
		update(t, m, limits)
		flows := make([]*Transfer, 12)
		for i := range flows {
			ip := "192.0.2.1:123"
			if i%2 == 1 {
				ip = "192.0.2.2:456"
			}
			flows[i] = open(t, m, ip, Download)
		}
		var totals [2]atomic.Int64
		for tick := 0; tick < 100; tick++ {
			c.advance(10 * time.Millisecond)
			var wg sync.WaitGroup
			for i, flow := range flows {
				wg.Add(1)
				go func(i int, flow *Transfer) {
					defer wg.Done()
					grant, _, _ := flow.take(100)
					totals[i%2].Add(int64(grant.n))
				}(i, flow)
			}
			wg.Wait()
		}
		if limits.TotalDownload > 0 && totals[0].Load()+totals[1].Load() > limits.TotalDownload+int64(capacity(limits.TotalDownload)) {
			t.Fatal("并发突破总配额")
		}
		for i := range totals {
			if limits.PerIPDownload > 0 && totals[i].Load() > limits.PerIPDownload+int64(capacity(limits.PerIPDownload)) {
				t.Fatal("同 IP 并发突破配额")
			}
		}
	}
}

func TestHotUpdateCancellationAndLowRate(t *testing.T) {
	c := newClock()
	m := newManager(c)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	flow := m.Open(ctx, "192.0.2.1:1", Download)
	defer flow.Close()
	if grant, err := flow.acquire(100); grant.n != 100 || err != nil {
		t.Fatal(grant, err)
	}
	update(t, m, config.BandwidthLimits{PerIPDownload: 1})
	done := make(chan error, 1)
	go func() {
		grant, err := flow.acquire(1000000)
		if err == nil && grant.n != 1 {
			err = errors.New("低速块未缩小")
		}
		done <- err
	}()
	waitTimer(t, c)
	c.advance(time.Second)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("低速永久等待")
	}
	go func() { _, err := flow.acquire(100); done <- err }()
	waitTimer(t, c)
	update(t, m, config.BandwidthLimits{})
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("取消限速没有唤醒")
	}
	update(t, m, config.BandwidthLimits{TotalDownload: 1})
	go func() { _, err := flow.acquire(100); done <- err }()
	waitTimer(t, c)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("请求取消没有解除等待")
	}
}

func TestUpdatePreservesDebtRefundAndIdleCleanup(t *testing.T) {
	c := newClock()
	m := newManager(c)
	update(t, m, config.BandwidthLimits{PerIPUpload: 1000})
	a := open(t, m, "192.0.2.1:1", Upload)
	grant, _, _ := a.take(100)
	a.refund(grant, 70)
	if a.peer.bucket.tokens != 70 {
		t.Fatal("未使用额度未退还")
	}
	update(t, m, config.BandwidthLimits{PerIPUpload: 500})
	if a.peer.bucket.tokens != 50 {
		t.Fatal("缩小额度未裁剪余额")
	}
	grant, _, _ = a.take(50)
	update(t, m, config.BandwidthLimits{PerIPUpload: 1000})
	a.refund(grant, 50)
	if a.peer.bucket.tokens != 0 {
		t.Fatal("旧预扣退款污染新配置")
	}
	if err := m.Update(config.BandwidthLimits{PerIPUpload: -1}); err == nil || a.peer.bucket.rate != 1000 {
		t.Fatal("接受了非法配置")
	}
	b := open(t, m, "192.0.2.2:1", Upload)
	b.Close()
	c.advance(6 * time.Minute)
	open(t, m, "192.0.2.3:1", Upload)
	if m.states[Upload].peers["192.0.2.1"] != a.peer {
		t.Fatal("活跃条目被清理")
	}
	if _, ok := m.states[Upload].peers["192.0.2.2"]; ok {
		t.Fatal("空闲条目未清理")
	}
}

type optimizedWriter struct {
	header http.Header
	copied bool
	short  bool
	count  int
}

func (w *optimizedWriter) Header() http.Header { return w.header }
func (w *optimizedWriter) WriteHeader(int)     {}
func (w *optimizedWriter) Write(p []byte) (int, error) {
	n := len(p)
	if w.short {
		n /= 2
	}
	w.count += n
	return n, nil
}
func (w *optimizedWriter) ReadFrom(io.Reader) (int64, error) { w.copied = true; return 0, nil }

func TestHTTPWrappers(t *testing.T) {
	m := newManager(newClock())
	flow := open(t, m, "192.0.2.1:1", Download)
	raw := &optimizedWriter{header: make(http.Header)}
	wrapped := flow.Writer(raw, http.MethodGet)
	if _, ok := wrapped.(http.Flusher); ok {
		t.Fatal("伪造 Flusher 能力")
	}
	if err := http.NewResponseController(wrapped).Flush(); !errors.Is(err, http.ErrNotSupported) {
		t.Fatal(err)
	}
	if n, err := io.Copy(wrapped, strings.NewReader(strings.Repeat("x", chunkSize*3))); err != nil || n != chunkSize*3 || raw.copied {
		t.Fatal(n, err, raw.copied)
	}
	if _, err := wrapped.(io.ReaderFrom).ReadFrom(strings.NewReader("abc")); err != nil || raw.copied {
		t.Fatal("ReadFrom 绕过包装")
	}
	raw.short = true
	if n, err := wrapped.Write([]byte("1234")); n != 2 || !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(n, err)
	}
	recorder := httptest.NewRecorder()
	flow.Writer(recorder, http.MethodGet).(http.Flusher).Flush()
	if !recorder.Flushed {
		t.Fatal("未透传 Flush")
	}
	update(t, m, config.BandwidthLimits{PerIPUpload: 1000})
	up := open(t, m, "192.0.2.1:1", Upload)
	reader := up.Reader(io.NopCloser(strings.NewReader("abc")))
	if n, err := reader.Read(make([]byte, 1000)); n != 3 || err != nil || up.peer.bucket.tokens != 97 {
		t.Fatal(n, err, up.peer.bucket.tokens)
	}
	reader.Close()
	update(t, m, config.BandwidthLimits{PerIPDownload: 1, TotalDownload: 1})
	if head := flow.Writer(recorder, http.MethodHead); head != recorder {
		t.Fatal("HEAD 仍使用实体限速包装器")
	}
	if flow.peer.bucket.tokens != 0 || flow.state.total.tokens != 0 {
		t.Fatal("HEAD 消耗或改变了下载额度")
	}
}
