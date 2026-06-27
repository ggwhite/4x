package feature

import (
	"errors"
	"fmt"
	"testing"
)

// mockStore 是 in-memory 的 Store 實作，供 feature package 測試使用，不碰檔案系統。
type mockStore struct {
	features map[string]Feature
	dirs     []string
	listErr  error // 非 nil 時 ListFeatures 回傳此 error，供錯誤傳播測試
}

func newMockStore() *mockStore {
	return &mockStore{features: map[string]Feature{}}
}

func (m *mockStore) DotDir() string              { return "/tmp/test" }
func (m *mockStore) FeatureDir(id string) string { return "/tmp/test/" + id }
func (m *mockStore) RoundDir(id string, round int) string {
	return fmt.Sprintf("/tmp/test/%s/rounds/round-%d", id, round)
}
func (m *mockStore) SaveFeature(f Feature) error { m.features[f.ID] = f; return nil }

func (m *mockStore) LoadFeature(id string) (Feature, error) {
	f, ok := m.features[id]
	if !ok {
		return Feature{}, fmt.Errorf("not found: %s", id)
	}
	return f, nil
}

func (m *mockStore) ListFeatures() ([]Feature, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	ff := make([]Feature, 0, len(m.features))
	for _, f := range m.features {
		ff = append(ff, f)
	}
	return ff, nil
}

func (m *mockStore) InitFeatureDir(id string) error { m.dirs = append(m.dirs, id); return nil }

func (m *mockStore) ResolveFeatureID(prefix string) (string, error) { return prefix, nil }

var defaultIDF = ResolveIDFormat("", 0)

func TestGenerateFeatureID(t *testing.T) {
	tests := []struct {
		num  int
		name string
		want string
	}{
		{1, "My Feature", "F001-my-feature"},
		{25, "Server Write API", "F025-server-write-api"},
		{100, "A very long feature name that exceeds the limit", "F100-a-very-long-feature"},
		{1000, "Four digit feature", "F1000-four-digit-feature"},
		{99999, "Five digit feature", "F99999-five-digit-feature"},
		{40, "Dashboard SPA file split — separate HTML, JS, CSS for maintainability", "F040-dashboard-spa-file"},
		{1, "single", "F001-single"},
		{1, "abcdefghijklmnopqrstuvwxyz", "F001-abcdefghijklmnopqrstuvw"},
	}
	for _, tt := range tests {
		got := GenerateFeatureID(tt.num, tt.name, defaultIDF)
		if got != tt.want {
			t.Errorf("GenerateFeatureID(%d, %q) = %q, want %q", tt.num, tt.name, got, tt.want)
		}
	}
}

func TestGenerateFeatureIDCustomPrefix(t *testing.T) {
	tests := []struct {
		idf  IDFormat
		num  int
		name string
		want string
	}{
		{ResolveIDFormat("ws-", 3), 1, "login page", "ws-001-login-page"},
		{ResolveIDFormat("ws-", 3), 42, "Auth Refactor", "ws-042-auth-refactor"},
		{ResolveIDFormat("TASK", 4), 7, "fix bug", "TASK0007-fix-bug"},
		{ResolveIDFormat("T-", 2), 99, "deploy", "T-99-deploy"},
	}
	for _, tt := range tests {
		got := GenerateFeatureID(tt.num, tt.name, tt.idf)
		if got != tt.want {
			t.Errorf("GenerateFeatureID(%d, %q, prefix=%q digits=%d) = %q, want %q",
				tt.num, tt.name, tt.idf.Prefix, tt.idf.Digits, got, tt.want)
		}
	}
}

func TestGenerateFeatureIDFromSlug(t *testing.T) {
	tests := []struct {
		num  int
		slug string
		want string
	}{
		{1, "my-custom-id", "F001-my-custom-id"},
		{40, "dashboard-spa-split", "F040-dashboard-spa-split"},
		{5, "A Very Long Custom Slug That Should Not Be Truncated", "F005-a-very-long-custom-slug-that-should-not-be-truncated"},
		{10, "UPPER--CASE", "F010-upper-case"},
		{43, "F043-dashboard-screenshot-gall", "F043-dashboard-screenshot-gall"},
		{43, "f043-dashboard-screenshot-gall", "F043-dashboard-screenshot-gall"},
	}
	for _, tt := range tests {
		got := GenerateFeatureIDFromSlug(tt.num, tt.slug, defaultIDF)
		if got != tt.want {
			t.Errorf("GenerateFeatureIDFromSlug(%d, %q) = %q, want %q", tt.num, tt.slug, got, tt.want)
		}
	}
}

func TestGenerateFeatureIDFromSlugCustomPrefix(t *testing.T) {
	idf := ResolveIDFormat("ws-", 3)
	tests := []struct {
		num  int
		slug string
		want string
	}{
		{1, "my-custom-id", "ws-001-my-custom-id"},
		{10, "ws-010-already-prefixed", "ws-010-already-prefixed"},
		{10, "WS-010-case-insensitive", "ws-010-case-insensitive"},
	}
	for _, tt := range tests {
		got := GenerateFeatureIDFromSlug(tt.num, tt.slug, idf)
		if got != tt.want {
			t.Errorf("GenerateFeatureIDFromSlug(%d, %q, prefix=%q) = %q, want %q",
				tt.num, tt.slug, idf.Prefix, got, tt.want)
		}
	}
}

func TestNextNumber(t *testing.T) {
	store := newMockStore()

	n, err := NextNumber(store, defaultIDF)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("got %d, want 1", n)
	}

	store.features["F003-test"] = Feature{ID: "F003-test", Name: "test"}
	n, err = NextNumber(store, defaultIDF)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("got %d, want 4", n)
	}

	store.features["F1000-four-digit"] = Feature{ID: "F1000-four-digit", Name: "four-digit"}
	n, err = NextNumber(store, defaultIDF)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1001 {
		t.Errorf("got %d, want 1001", n)
	}
}

func TestNextNumberCustomPrefix(t *testing.T) {
	idf := ResolveIDFormat("ws-", 3)
	store := newMockStore()

	n, err := NextNumber(store, idf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("got %d, want 1", n)
	}

	store.features["ws-005-login"] = Feature{ID: "ws-005-login", Name: "login"}
	// F-prefix features 不應被 ws- prefix 掃到
	store.features["F001-unrelated"] = Feature{ID: "F001-unrelated", Name: "unrelated"}

	n, err = NextNumber(store, idf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 6 {
		t.Errorf("got %d, want 6", n)
	}

	// 舊格式（無 slug）：ws-094 應被正確掃到
	store2 := newMockStore()
	store2.features["ws-094"] = Feature{ID: "ws-094", Name: "legacy"}
	store2.features["ws-093"] = Feature{ID: "ws-093", Name: "legacy2"}
	n, err = NextNumber(store2, idf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 95 {
		t.Errorf("legacy no-slug: got %d, want 95", n)
	}
}

// TestNextNumberListError 驗證 ListFeatures 失敗時 NextNumber 將 error 往外傳，
// 不再 silent fallback 到流水號 1（否則會與既有 feature 碰撞並覆蓋其 YAML）。
func TestNextNumberListError(t *testing.T) {
	sentinel := errors.New("disk on fire")
	store := newMockStore()
	store.listErr = sentinel

	n, err := NextNumber(store, defaultIDF)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error %v should wrap sentinel", err)
	}
	if n == 1 {
		t.Errorf("got %d, want non-1 on error (must not silent fallback to 1)", n)
	}
}

func TestResolveIDFormat(t *testing.T) {
	idf := ResolveIDFormat("", 0)
	if idf.Prefix != DefaultIDPrefix {
		t.Errorf("default prefix = %q, want %q", idf.Prefix, DefaultIDPrefix)
	}
	if idf.Digits != DefaultIDDigits {
		t.Errorf("default digits = %d, want %d", idf.Digits, DefaultIDDigits)
	}

	idf = ResolveIDFormat("ws-", 4)
	if idf.Prefix != "ws-" {
		t.Errorf("prefix = %q, want %q", idf.Prefix, "ws-")
	}
	if idf.Digits != 4 {
		t.Errorf("digits = %d, want 4", idf.Digits)
	}
}

func TestFormatDisplayName(t *testing.T) {
	tests := []struct {
		idf  IDFormat
		num  int
		name string
		want string
	}{
		{defaultIDF, 1, "My Feature", "F001: My Feature"},
		{ResolveIDFormat("ws-", 3), 42, "Login Page", "ws-042: Login Page"},
		{ResolveIDFormat("TASK", 4), 7, "Fix Bug", "TASK0007: Fix Bug"},
	}
	for _, tt := range tests {
		got := tt.idf.FormatDisplayName(tt.num, tt.name)
		if got != tt.want {
			t.Errorf("FormatDisplayName(%d, %q) = %q, want %q", tt.num, tt.name, got, tt.want)
		}
	}
}
