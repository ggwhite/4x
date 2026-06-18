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
		got := GenerateFeatureID(tt.num, tt.name)
		if got != tt.want {
			t.Errorf("GenerateFeatureID(%d, %q) = %q, want %q", tt.num, tt.name, got, tt.want)
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
		got := GenerateFeatureIDFromSlug(tt.num, tt.slug)
		if got != tt.want {
			t.Errorf("GenerateFeatureIDFromSlug(%d, %q) = %q, want %q", tt.num, tt.slug, got, tt.want)
		}
	}
}

func TestNextNumber(t *testing.T) {
	store := newMockStore()

	n, err := NextNumber(store)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("got %d, want 1", n)
	}

	store.features["F003-test"] = Feature{ID: "F003-test", Name: "test"}
	n, err = NextNumber(store)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("got %d, want 4", n)
	}

	store.features["F1000-four-digit"] = Feature{ID: "F1000-four-digit", Name: "four-digit"}
	n, err = NextNumber(store)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1001 {
		t.Errorf("got %d, want 1001", n)
	}
}

// TestNextNumberListError 驗證 ListFeatures 失敗時 NextNumber 將 error 往外傳，
// 不再 silent fallback 到流水號 1（否則會與既有 feature 碰撞並覆蓋其 YAML）。
func TestNextNumberListError(t *testing.T) {
	sentinel := errors.New("disk on fire")
	store := newMockStore()
	store.listErr = sentinel

	n, err := NextNumber(store)
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
