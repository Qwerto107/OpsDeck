package app

import (
	"fmt"
	"testing"
)

func TestMultiSortAndNullsLast(t *testing.T) {
	rows := []Resource{{ID: 1, AccountID: 1, Region: "ap-b", Name: "a", InstanceID: "1", Tags: map[string]string{}}, {ID: 2, AccountID: 1, Region: "ap-a", Name: "c", InstanceID: "2", Tags: map[string]string{"env": "prod"}}, {ID: 3, AccountID: 1, Region: "ap-a", Name: "a", InstanceID: "3", Tags: map[string]string{"env": "prod"}}, {ID: 4, AccountID: 1, Region: "ap-a", Name: "b", InstanceID: "4", Tags: map[string]string{}}}
	rules, e := ParseSort(`[{"field":"region","desc":false},{"field":"tag","key":"env","desc":true},{"field":"name","desc":false}]`)
	if e != nil {
		t.Fatal(e)
	}
	SortResources(rows, rules)
	for i, id := range []uint64{3, 2, 4, 1} {
		if rows[i].ID != id {
			t.Fatalf("排序第 %d 位错误：%d", i, rows[i].ID)
		}
	}
}
func TestSortRejectsInvalidRules(t *testing.T) {
	for _, raw := range []string{`[{"field":"name);DROP TABLE users"}]`, `[{"field":"tag"}]`, `[{"field":"name"},{"field":"name"}]`, `[{"field":"tag","key":"env"},{"field":"tag","key":"env"}]`} {
		if _, e := ParseSort(raw); e == nil {
			t.Fatalf("接受了无效排序：%s", raw)
		}
	}
}

func TestMetricSortUsesNumericUsageAndKeepsMissingLast(t *testing.T) {
	for _, field := range []string{"cpu", "memory", "disk"} {
		for _, descending := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/desc=%t", field, descending), func(t *testing.T) {
				rows := []Resource{}
				for i, value := range []float64{9, 80, 0, 80} {
					name := "b"
					if i == 3 {
						name = "a"
					}
					rows = append(rows, Resource{ID: uint64(i + 1), Name: name, CPU: 128 - i, Metrics: map[string]Metric{field: {Value: &value, Status: "stale"}}})
				}
				rows = append(rows, Resource{ID: 5, Name: "missing"})
				rules, err := ParseSort(fmt.Sprintf(`[{"field":%q,"desc":%t},{"field":"name"}]`, field, descending))
				if err != nil {
					t.Fatal(err)
				}
				SortResources(rows, rules)
				want := []uint64{3, 1, 4, 2, 5}
				if descending {
					want = []uint64{4, 2, 1, 3, 5}
				}
				for i, id := range want {
					if rows[i].ID != id {
						t.Fatalf("第 %d 项得到 %d，期望 %d", i, rows[i].ID, id)
					}
				}
			})
		}
	}
}
