package main

import (
	"context"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestServeDoesNotStartWorkerWhenPortIsOccupied(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var started atomic.Bool
	err = serve(context.Background(), &http.Server{Addr: listener.Addr().String()}, func(context.Context) { started.Store(true) })
	if err == nil || started.Load() {
		t.Fatal("绑定端口失败时仍启动了 Worker")
	}
}

func TestServeWaitsForWorkerBeforeReturning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, stopping, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, &http.Server{Addr: "127.0.0.1:0"}, func(ctx context.Context) {
			close(started)
			<-ctx.Done()
			close(stopping)
			<-release
		})
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("Worker 未启动")
	}
	cancel()
	select {
	case <-stopping:
	case <-time.After(3 * time.Second):
		t.Fatal("Worker 未收到退出信号")
	}
	select {
	case <-done:
		t.Fatal("未等待 Worker 收尾便返回")
	default:
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("服务未完成退出")
	}
}

func TestServeAPIOnlyStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, &http.Server{Addr: "127.0.0.1:0"}, nil) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("独立 API 未完成退出")
	}
}
