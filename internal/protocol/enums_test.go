package protocol

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

func TestAllPhases(t *testing.T) {
	phases := AllPhases()
	assertNonEmptyNoDup(t, "AllPhases", phases)
	if len(phases) != 15 {
		t.Errorf("AllPhases() 應回傳 15 個值，實際 %d 個：%v", len(phases), phases)
	}
}

func TestAllRoles(t *testing.T) {
	roles := AllRoles()
	assertNonEmptyNoDup(t, "AllRoles", roles)
	if len(roles) != 8 {
		t.Errorf("AllRoles() 應回傳 8 個值，實際 %d 個：%v", len(roles), roles)
	}
}

func TestAllSubPhases(t *testing.T) {
	subPhases := AllSubPhases()
	assertNonEmptyNoDup(t, "AllSubPhases", subPhases)
	if len(subPhases) != 4 {
		t.Errorf("AllSubPhases() 應回傳 4 個值，實際 %d 個：%v", len(subPhases), subPhases)
	}
}

func TestAllEventTypes(t *testing.T) {
	types := AllEventTypes()
	assertNonEmptyNoDup(t, "AllEventTypes", types)
	if len(types) != 15 {
		t.Errorf("AllEventTypes() 應回傳 15 個值，實際 %d 個：%v", len(types), types)
	}
	for _, excluded := range []string{"phase-end", "step", "verify", "scope-check", "blocked", "heartbeat", "subtask-done"} {
		for _, v := range types {
			if v == excluded {
				t.Errorf("AllEventTypes() 不應包含無 emitter 的值 %q", excluded)
			}
		}
	}
}

func TestAllNotifyLevels(t *testing.T) {
	levels := AllNotifyLevels()
	assertNonEmptyNoDup(t, "AllNotifyLevels", levels)
	if len(levels) != 3 {
		t.Errorf("AllNotifyLevels() 應回傳 3 個值，實際 %d 個：%v", len(levels), levels)
	}
}
