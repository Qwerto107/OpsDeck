package app

import (
	"context"
	"testing"
)

func TestTencentServerAddresses(t *testing.T) {
	for _, site := range []string{"china", "international"} {
		for _, kind := range []string{"cvm", "lighthouse"} {
			for _, assigned := range []bool{true, false} {
				name := site + "/" + kind + "/unassigned"
				if assigned {
					name = site + "/" + kind + "/assigned"
				}
				t.Run(name, func(t *testing.T) {
					instance := map[string]any{
						"InstanceId": "test-instance", "InstanceName": "test", "InstanceState": "RUNNING",
						"CPU": 2, "Memory": 4, "InstanceType": "S5.MEDIUM4", "BundleId": "bundle-test",
						"SystemDisk": map[string]any{"DiskSize": 40},
						"Tags":       []any{map[string]any{"Key": "env", "Value": "test"}},
					}
					publicKey, privateKey := "PublicIpAddresses", "PrivateIpAddresses"
					if kind == "lighthouse" {
						publicKey, privateKey = "PublicAddresses", "PrivateAddresses"
					}
					wantPublic, wantPrivate := "", ""
					if assigned {
						instance[publicKey] = []string{"203.0.113.10", "203.0.113.11"}
						instance[privateKey] = []string{"10.0.0.10", "10.0.0.11"}
						wantPublic, wantPrivate = "203.0.113.10, 203.0.113.11", "10.0.0.10, 10.0.0.11"
					} else {
						instance[publicKey] = nil
						instance[privateKey] = []string{}
					}
					client, ac := monitorClient(t, func(map[string]any) map[string]any {
						return map[string]any{"InstanceSet": []any{instance}}
					})
					ac.Site = site
					rows, err := client.List(context.Background(), ac, "ap-singapore", kind)
					if err != nil || len(rows) != 1 {
						t.Fatalf("实例查询失败: count=%d err=%v", len(rows), err)
					}
					r := rows[0]
					if r.PublicIP != wantPublic || r.PrivateIP != wantPrivate {
						t.Fatal("公私网地址解析错误")
					}
					if r.CPU != 2 || r.Memory != 4 || r.Disk != 40 || r.Tags["env"] != "test" {
						t.Fatal("实例配置或标签解析错误")
					}
				})
			}
		}
	}
}
