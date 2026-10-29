package app

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const monitorConcurrency = 4

type cloudRequests struct {
	once      sync.Once
	transport http.RoundTripper
	accounts  sync.Map
}

func (r *cloudRequests) httpTransport() http.RoundTripper {
	r.once.Do(func() {
		r.transport = http.DefaultTransport
		if transport, ok := http.DefaultTransport.(*http.Transport); ok {
			pool := transport.Clone()
			pool.MaxIdleConns = 100
			pool.MaxIdleConnsPerHost = 16
			r.transport = pool
		}
	})
	return r.transport
}

type monitorGate struct {
	slots chan struct{}
	mu    sync.Mutex
	next  map[string]time.Time
}

func (r *cloudRequests) acquireMonitor(ctx context.Context, ac CloudAccount, action string) (func(), error) {
	switch action {
	case "GetMonitorData", "DescribeMetricList", "DescribeMetricMetaList", "DescribeDBInstancePerformance":
	default:
		return func() {}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%d/%s", ac.ID, ac.Site)
	value, _ := r.accounts.LoadOrStore(key, &monitorGate{slots: make(chan struct{}, monitorConcurrency), next: map[string]time.Time{}})
	gate := value.(*monitorGate)
	select {
	case gate.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release := func() { <-gate.slots }
	// 地域间共享账号的并发额度；同接口均匀发出请求，避免并发批次集中触发限频。
	interval := 50 * time.Millisecond
	if action == "DescribeDBInstancePerformance" {
		interval = 200 * time.Millisecond
	}
	gate.mu.Lock()
	start := time.Now()
	if next := gate.next[action]; next.After(start) {
		start = next
	}
	gate.next[action] = start.Add(interval)
	gate.mu.Unlock()
	if delay := time.Until(start); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			release()
			return nil, ctx.Err()
		}
	}
	return release, nil
}

func runMonitorQueries(queries []func()) {
	var pending sync.WaitGroup
	slots := make(chan struct{}, monitorConcurrency)
	for _, query := range queries {
		slots <- struct{}{}
		pending.Add(1)
		go func() {
			defer pending.Done()
			defer func() { <-slots }()
			query()
		}()
	}
	pending.Wait()
}
