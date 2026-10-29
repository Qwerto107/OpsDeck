package app

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestMonitorGatePacesRequestsAndSeparatesAccounts(t *testing.T) {
	var requests cloudRequests
	account := CloudAccount{ID: 1}
	var releases []func()
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	started := time.Now()
	for range monitorConcurrency {
		release, err := requests.acquireMonitor(context.Background(), account, "GetMonitorData")
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	if time.Since(started) < 140*time.Millisecond {
		t.Fatal("同账号同接口请求未按频率限制发出")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := requests.acquireMonitor(ctx, account, "GetMonitorData"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("超过账号并发上限后没有等待，或不能取消")
	}
	// 使用独立期限，确认其他账号不受已满账号的并发槽位影响。
	otherCtx, otherCancel := context.WithTimeout(context.Background(), time.Second)
	defer otherCancel()
	other, err := requests.acquireMonitor(otherCtx, CloudAccount{ID: 2}, "GetMonitorData")
	if err != nil {
		t.Fatal("其他账号被无关账号的并发限制阻塞")
	}
	other()
}

func TestProviderMetricsAreConcurrent(t *testing.T) {
	for _, provider := range []string{"tencent", "alibaba"} {
		t.Run(provider, func(t *testing.T) {
			started := make(chan struct{}, 3)
			release := make(chan struct{})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			wait := func() {
				started <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			var cloud Cloud
			var ac CloudAccount
			if provider == "tencent" {
				cloud, ac = monitorClient(t, func(map[string]any) map[string]any {
					wait()
					return map[string]any{"DataPoints": []any{}}
				})
			} else {
				cloud, ac = alibabaFixture(t, func(*http.Request) map[string]any {
					wait()
					return map[string]any{"Success": true, "Datapoints": "[]"}
				})
			}
			kind := "cvm"
			if provider == "alibaba" {
				kind = "ecs"
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				cloud.MonitorBatch(ctx, ac, []Resource{{AccountID: ac.ID, Region: "ap-test", Kind: kind, State: "RUNNING", InstanceID: "test"}}, []string{"cpu", "memory", "disk"})
			}()
			var once sync.Once
			finish := func() { once.Do(func() { close(release) }); <-done }
			defer finish()
			for range 3 {
				select {
				case <-started:
				case <-ctx.Done():
					t.Fatal("指标查询仍按串行执行")
				}
			}
			finish()
		})
	}
}
