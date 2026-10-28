package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func tencentDatabaseProduct(kind string) (string, string) {
	if kind == "mysql" {
		return "cdb", "2017-03-20"
	}
	return "postgres", "2017-03-12"
}

func databaseResource(ac CloudAccount, region, kind, id, name string) Resource {
	if name == "" {
		name = id
	}
	engine := "MySQL"
	if kind == "postgresql" {
		engine = "PostgreSQL"
	}
	return Resource{AccountID: ac.ID, Provider: ac.Provider, Region: region, Kind: kind, InstanceID: id, Name: name, Tags: map[string]string{}, Metrics: map[string]Metric{}, SyncedAt: time.Now(), Database: &DatabaseInfo{Engine: engine, Endpoints: []DatabaseEndpoint{}}}
}

func readDatabaseTags(r *Resource, items []any, key, value string) {
	for _, item := range items {
		m, ok := item.(map[string]any)
		if ok && str(m, key) != "" {
			r.Tags[str(m, key)] = str(m, value)
		}
	}
}

func addDatabaseEndpoint(r *Resource, address string, port int, network string) {
	if address == "" {
		return
	}
	r.Database.Endpoints = append(r.Database.Endpoints, DatabaseEndpoint{Address: address, Port: port, Network: network})
}

func (t *Tencent) listDatabases(ctx context.Context, ac CloudAccount, region, kind string) ([]Resource, error) {
	service, version := tencentDatabaseProduct(kind)
	rows := []Resource{}
	for offset := 0; ; offset += 100 {
		body, err := t.call(ctx, ac, region, service, version, "DescribeDBInstances", map[string]any{"Offset": offset, "Limit": 100})
		if err != nil {
			return nil, err
		}
		field := "Items"
		if kind == "postgresql" {
			field = "DBInstanceSet"
		}
		items, ok := body[field].([]any)
		if !ok {
			return nil, &CloudError{Code: "InvalidDatabaseListResponse"}
		}
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, &CloudError{Code: "InvalidDatabaseListResponse"}
			}
			id, name := str(m, "InstanceId"), str(m, "InstanceName")
			if kind == "postgresql" {
				id, name = str(m, "DBInstanceId"), str(m, "DBInstanceName")
			}
			if id == "" {
				return nil, &CloudError{Code: "InvalidDatabaseListResponse"}
			}
			r := databaseResource(ac, region, kind, id, name)
			if kind == "mysql" {
				r.CPU = num(m, "Cpu")
				r.Database.MemoryGB = float64(num(m, "Memory")) / 1024
				r.Disk = num(m, "Volume")
				r.Spec = str(m, "DeviceClass")
				r.Database.Version = str(m, "EngineVersion")
				r.State = map[int]string{0: "CREATING", 1: "RUNNING", 4: "ISOLATING", 5: "ISOLATED"}[num(m, "Status")]
				if r.State == "" {
					r.State = fmt.Sprintf("STATUS_%d", num(m, "Status"))
				}
				if r.State == "RUNNING" && num(m, "TaskStatus") != 0 {
					r.State = "UPDATING"
				}
				addDatabaseEndpoint(&r, str(m, "Vip"), num(m, "Vport"), "private")
				addDatabaseEndpoint(&r, str(m, "WanDomain"), num(m, "WanPort"), "public")
				readDatabaseTags(&r, array(m, "TagList"), "TagKey", "TagValue")
			} else {
				r.CPU = num(m, "DBInstanceCpu")
				r.Database.MemoryGB = float64(num(m, "DBInstanceMemory"))
				r.Disk = num(m, "DBInstanceStorage")
				r.Spec = str(m, "DBInstanceClass")
				r.Database.Version = str(m, "DBVersion")
				r.State = strings.ToUpper(strings.ReplaceAll(str(m, "DBInstanceStatus"), " ", "_"))
				for _, item := range array(m, "DBInstanceNetInfo") {
					n, _ := item.(map[string]any)
					status := str(n, "Status")
					if status != "opened" && status != "2" {
						continue
					}
					address := str(n, "Address")
					if address == "" {
						address = str(n, "Ip")
					}
					network := str(n, "NetType")
					if network == "public" || network == "3" {
						network = "public"
					} else if network == "private" || network == "inner" || network == "1" || network == "2" {
						network = "private"
					} else {
						network = "unknown"
					}
					addDatabaseEndpoint(&r, address, num(n, "Port"), network)
				}
				readDatabaseTags(&r, array(m, "TagList"), "TagKey", "TagValue")
			}
			if _, ok := m["TagList"]; !ok {
				r.TagsError = "云接口未返回标签字段"
			}
			r.Memory = int(r.Database.MemoryGB)
			if r.State == "" {
				r.State = "UNKNOWN"
			}
			rows = append(rows, r)
		}
		if len(items) < 100 {
			break
		}
	}
	return rows, nil
}

func (a *Alibaba) listDatabases(ctx context.Context, ac CloudAccount, region, kind string) ([]Resource, error) {
	engine := "MySQL"
	if kind == "postgresql" {
		engine = "PostgreSQL"
	}
	rows := []Resource{}
	for page := 1; ; page++ {
		body, err := a.call(ctx, ac, region, "rds", "2014-08-15", "DescribeDBInstances", map[string]string{"Engine": engine, "PageNumber": strconv.Itoa(page), "PageSize": "100"})
		if err != nil {
			return nil, err
		}
		items, ok := obj(body, "Items")["DBInstance"].([]any)
		if !ok {
			return nil, &CloudError{Code: "InvalidDatabaseListResponse"}
		}
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok || str(m, "DBInstanceId") == "" || !strings.EqualFold(str(m, "Engine"), engine) {
				return nil, &CloudError{Code: "InvalidDatabaseListResponse"}
			}
			id := str(m, "DBInstanceId")
			r := databaseResource(ac, region, kind, id, str(m, "DBInstanceDescription"))
			// 仅读取并保存展示字段，不保存 API 可能附带的安全配置或密码相关字段。
			detail, err := a.call(ctx, ac, region, "rds", "2014-08-15", "DescribeDBInstanceAttribute", map[string]string{"DBInstanceId": id})
			if err != nil {
				return nil, err
			}
			attrs := array(obj(detail, "Items"), "DBInstanceAttribute")
			if len(attrs) != 1 {
				return nil, &CloudError{Code: "InvalidDatabaseDetailResponse"}
			}
			attr, _ := attrs[0].(map[string]any)
			if str(attr, "DBInstanceId") != id {
				return nil, &CloudError{Code: "InvalidDatabaseDetailResponse"}
			}
			r.CPU, _ = strconv.Atoi(str(attr, "DBInstanceCPU"))
			r.Database.MemoryGB = float64(num(attr, "DBInstanceMemory")) / 1024
			r.Memory = int(r.Database.MemoryGB)
			r.Disk = num(attr, "DBInstanceStorage")
			r.Spec = str(attr, "DBInstanceClass")
			r.Database.Version = str(attr, "EngineVersion")
			r.State = strings.ToUpper(str(attr, "DBInstanceStatus"))
			if r.State == "" {
				r.State = "UNKNOWN"
			}
			netinfo, err := a.call(ctx, ac, region, "rds", "2014-08-15", "DescribeDBInstanceNetInfo", map[string]string{"DBInstanceId": id})
			if err != nil {
				return nil, err
			}
			nets, ok := obj(netinfo, "DBInstanceNetInfos")["DBInstanceNetInfo"].([]any)
			if !ok {
				return nil, &CloudError{Code: "InvalidDatabaseEndpointResponse"}
			}
			for _, item := range nets {
				n, _ := item.(map[string]any)
				address := str(n, "ConnectionString")
				if address == "" {
					address = str(n, "IPAddress")
				}
				port, _ := strconv.Atoi(str(n, "Port"))
				network := "unknown"
				switch str(n, "IPType") {
				case "Public":
					network = "public"
				case "Private", "Inner":
					network = "private"
				}
				addDatabaseEndpoint(&r, address, port, network)
			}
			tags, err := a.call(ctx, ac, region, "rds", "2014-08-15", "DescribeTags", map[string]string{"DBInstanceId": id, "ResourceType": "INSTANCE"})
			if err != nil {
				r.TagsError = "数据库标签查询失败：" + err.Error()
			} else if items, ok := obj(tags, "Items")["TagInfos"].([]any); ok {
				readDatabaseTags(&r, items, "TagKey", "TagValue")
			} else {
				r.TagsError = "云接口未返回标签字段"
			}
			rows = append(rows, r)
		}
		if len(items) < 100 {
			break
		}
	}
	return rows, nil
}
