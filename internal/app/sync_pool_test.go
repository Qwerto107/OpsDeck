package app

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

type poolCloud struct {
	fakeCloud
	active  atomic.Int32
	peak    atomic.Int32
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (f *poolCloud) Kinds(CloudAccount) []string { return []string{"ecs"} }
func (f *poolCloud) Regions(context.Context, CloudAccount, string) ([]string, error) {
	regions := []string{}
	for i := 0; i < 12; i++ {
		regions = append(regions, fmt.Sprintf("region-%d", i))
	}
	return regions, nil
}
func (f *poolCloud) List(ctx context.Context, _ CloudAccount, _ string, kind string) ([]Resource, error) {
	n := f.active.Add(1)
	defer f.active.Add(-1)
	for {
		old := f.peak.Load()
		if old >= n || f.peak.CompareAndSwap(old, n) {
			break
		}
	}
	f.calls.Add(1)
	f.started <- struct{}{}
	select {
	case <-f.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if kind != "ecs" {
		return nil, fmt.Errorf("unexpected resource kind")
	}
	return nil, nil
}
func TestPostgresSyncConcurrencyBoundedAndProviderNeutral(t *testing.T) {
	a, _ := integrationApp(t)
	ac := CloudAccount{Name: "pool", Provider: "future-provider", SecretID: "encrypted", SecretKey: "encrypted", Regions: []string{}, Enabled: true}
	if err := a.DB.Create(&ac).Error; err != nil {
		t.Fatal(err)
	}
	task := Task{AccountID: &ac.ID, Action: "sync", Status: "queued", NextRun: time.Now()}
	if err := a.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	f := &poolCloud{started: make(chan struct{}, 16), release: make(chan struct{})}
	a.Cloud = f
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { a.syncResources(ctx, task, ac); close(done) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("同步未退出")
		}
	}()
	for i := 0; i < 4; i++ {
		select {
		case <-f.started:
		case <-time.After(3 * time.Second):
			t.Fatal("没有并发执行地域查询")
		}
	}
	close(f.release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("同步未完成")
	}
	if f.peak.Load() != 4 || f.calls.Load() != 12 {
		t.Fatalf("并发上限或资源类型适配错误: peak=%d calls=%d", f.peak.Load(), f.calls.Load())
	}
}
