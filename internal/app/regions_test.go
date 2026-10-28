package app

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAvailableRegionsValidatesAndFilters(t *testing.T) {
	got, err := availableRegions(map[string]any{"RegionSet": []any{
		map[string]any{"Region": "ap-beijing", "RegionState": "AVAILABLE"},
		map[string]any{"Region": "ap-beijing", "RegionState": "AVAILABLE"},
		map[string]any{"Region": "ap-closed", "RegionState": "UNAVAILABLE"},
	}})
	if err != nil || len(got) != 1 || got[0] != "ap-beijing" {
		t.Fatalf("地域过滤失败: %v %v", got, err)
	}
	for _, body := range []map[string]any{{}, {"RegionSet": []any{map[string]any{"Region": "ap-beijing"}}}} {
		if _, err := availableRegions(body); err == nil {
			t.Fatal("无效响应被当作成功")
		}
	}
}

type regionCloud struct {
	mu sync.Mutex
	fakeCloud
	sets           map[string][]string
	discoveryError bool
	listed         []string
}

func (f *regionCloud) Regions(_ context.Context, _ CloudAccount, kind string) ([]string, error) {
	if f.discoveryError && kind == "cvm" {
		return nil, &CloudError{Code: "UnauthorizedOperation"}
	}
	return f.sets[kind], nil
}
func (f *regionCloud) List(_ context.Context, _ CloudAccount, region, kind string) ([]Resource, error) {
	f.mu.Lock()
	f.listed = append(f.listed, kind+":"+region)
	f.mu.Unlock()
	return nil, nil
}
func TestPostgresAutomaticRegionsStayProductSpecific(t *testing.T) {
	a, _ := integrationApp(t)
	ac := CloudAccount{Name: "test", Provider: "tencent", SecretID: "encrypted", SecretKey: "encrypted", Regions: []string{"ap-old"}, Enabled: true}
	if err := a.DB.Create(&ac).Error; err != nil {
		t.Fatal(err)
	}
	task := Task{AccountID: &ac.ID, Action: "sync", Status: "queued", NextRun: time.Now()}
	if err := a.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	f := &regionCloud{sets: map[string][]string{"cvm": {"ap-new"}, "lighthouse": {"ap-light"}}}
	a.Cloud = f
	a.syncResources(context.Background(), task, ac)
	sort.Strings(f.listed)
	if strings.Join(f.listed, ",") != "cvm:ap-new,lighthouse:ap-light" {
		t.Fatalf("错误的产品地域组合: %v", f.listed)
	}
	a.DB.First(&ac, ac.ID)
	if strings.Join(ac.Regions, ",") != "ap-light,ap-new" {
		t.Fatalf("未保存自动发现地域: %v", ac.Regions)
	}
	// 一个产品发现失败时保留历史地域，继续同步另一个产品。
	task.ID = 0
	task.Status = "queued"
	if err := a.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	f.listed = nil
	f.discoveryError = true
	f.sets["lighthouse"] = []string{"ap-added"}
	a.syncResources(context.Background(), task, ac)
	a.DB.First(&ac, ac.ID)
	a.DB.First(&task, task.ID)
	if strings.Join(f.listed, ",") != "lighthouse:ap-added" || task.Status != "failed" || strings.Join(ac.Regions, ",") != "ap-added,ap-light,ap-new" {
		t.Fatalf("发现失败未保留已有范围: %v %v %s", f.listed, ac.Regions, task.Status)
	}
}
