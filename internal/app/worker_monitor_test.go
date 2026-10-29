package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

type parallelRefreshCloud struct {
	fakeCloud
	rows    []Resource
	started chan uint64
	release map[uint64]chan struct{}
}

func (f *parallelRefreshCloud) List(ctx context.Context, account CloudAccount, _, _ string) ([]Resource, error) {
	f.started <- account.ID
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-f.release[account.ID]:
	}
	rows := []Resource{}
	for _, row := range f.rows {
		if row.AccountID == account.ID {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (f *parallelRefreshCloud) MonitorBatch(_ context.Context, _ CloudAccount, rows []Resource, _ []string) map[string]map[string]Metric {
	result := map[string]map[string]Metric{}
	for _, row := range rows {
		value, sampled := 1.0, time.Now()
		result[row.InstanceID] = map[string]Metric{"cpu": {Value: &value, SampledAt: &sampled, Status: "ok"}}
	}
	return result
}

func TestPostgresMonitorAccountsRunConcurrentlyInOrder(t *testing.T) {
	a, admin := integrationApp(t)
	accounts := []CloudAccount{{Name: "parallel-a", Enabled: true, Regions: []string{}}, {Name: "parallel-b", Enabled: true, Regions: []string{}}}
	if err := a.DB.Create(&accounts).Error; err != nil {
		t.Fatal(err)
	}
	rows := []Resource{}
	for i, account := range []uint64{accounts[0].ID, accounts[0].ID, accounts[1].ID} {
		row := Resource{AccountID: account, Kind: "cvm", Region: "ap-test", InstanceID: fmtID(uint64(i + 1)), State: "RUNNING", Tags: map[string]string{}, Metrics: map[string]Metric{}}
		if err := a.DB.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	audit := Audit{UserID: &admin.ID, Actor: admin.Username, Action: "resources.monitor", Result: "accepted"}
	if err := a.DB.Create(&audit).Error; err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-time.Second)
	tasks := []Task{}
	for _, row := range rows {
		task := Task{AccountID: &row.AccountID, AuditID: &audit.ID, ResourceIDs: []uint64{row.ID}, Action: "monitor", Status: "queued", NextRun: stamp}
		if err := a.DB.Create(&task).Error; err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, task)
	}
	cloud := &parallelRefreshCloud{rows: rows, started: make(chan uint64, 4), release: map[uint64]chan struct{}{accounts[0].ID: make(chan struct{}), accounts[1].ID: make(chan struct{})}}
	a.Cloud = cloud
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); a.RunWorker(ctx) }()
	defer func() { cancel(); <-done }()
	started := map[uint64]bool{}
	for range 2 {
		select {
		case account := <-cloud.started:
			if started[account] {
				t.Fatal("同账号监控任务并发执行")
			}
			started[account] = true
		case <-time.After(4 * time.Second):
			t.Fatal("不同账号的监控任务未同时启动")
		}
	}
	select {
	case <-cloud.started:
		t.Fatal("同账号的后续任务绕过了当前任务")
	case <-time.After(100 * time.Millisecond):
	}
	close(cloud.release[accounts[0].ID])
	select {
	case account := <-cloud.started:
		if account != accounts[0].ID {
			t.Fatal("未按账号继续处理后续任务")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("前序任务结束后没有继续处理同账号任务")
	}
	var first Task
	if err := a.DB.First(&first, tasks[0].ID).Error; err != nil || first.Status != "succeeded" {
		t.Fatal("账号的前序任务尚未完成就处理了后序任务")
	}
}

func TestPostgresMonitorCapacityDoesNotReorderAccountOperations(t *testing.T) {
	a, _ := integrationApp(t)
	ac := CloudAccount{Name: "ordered", Enabled: true, Regions: []string{}}
	if err := a.DB.Create(&ac).Error; err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-time.Second)
	for _, action := range []string{"monitor", "reboot"} {
		if err := a.DB.Create(&Task{AccountID: &ac.ID, Action: action, Status: "queued", NextRun: stamp}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.claimTask(context.Background(), false); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("监控槽位已满时，操作绕过了同账号前序监控任务")
	}
}
