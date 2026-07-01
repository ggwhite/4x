package feature

import "testing"

// assertNonEmptyNoDup 驗證任一底層為 string 的 enum accessor 回傳非空、無重複值。
func assertNonEmptyNoDup[T ~string](t *testing.T, name string, values []T) {
	t.Helper()
	if len(values) == 0 {
		t.Fatalf("%s() 回傳空 slice", name)
	}
	seen := make(map[T]bool, len(values))
	for _, v := range values {
		if seen[v] {
			t.Fatalf("%s() 含重複值 %q", name, string(v))
		}
		seen[v] = true
	}
}

func TestAllStatuses(t *testing.T) {
	statuses := AllStatuses()
	assertNonEmptyNoDup(t, "AllStatuses", statuses)
	if len(statuses) != 8 {
		t.Errorf("AllStatuses() 應回傳 8 個值，實際 %d 個：%v", len(statuses), statuses)
	}
}

func TestAllSubtaskStatuses(t *testing.T) {
	statuses := AllSubtaskStatuses()
	assertNonEmptyNoDup(t, "AllSubtaskStatuses", statuses)
	if len(statuses) != 5 {
		t.Errorf("AllSubtaskStatuses() 應回傳 5 個值，實際 %d 個：%v", len(statuses), statuses)
	}
	found := false
	for _, v := range statuses {
		if v == "ready-for-review" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("AllSubtaskStatuses() 應包含 ready-for-review")
	}
}
