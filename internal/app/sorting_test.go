package app

import "testing"

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
