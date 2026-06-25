package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLogSortKey_DeepReviewGrouping(t *testing.T) {
	// 平行 sub-reviewer → synthesizer → self-heal 的預期顯示順序。
	names := []string{
		"round-1-synthesizer.log",
		"round-1-deep-reviewer-2.log",
		"round-1-deep-fix-1.log",
		"round-1-deep-reviewer-1.log",
		"round-1-deep-reverify-1.log",
	}
	want := []string{
		"round-1-deep-reviewer-1.log",
		"round-1-deep-reviewer-2.log",
		"round-1-synthesizer.log",
		"round-1-deep-fix-1.log",
		"round-1-deep-reverify-1.log",
	}
	// 以 logSortKey 排序後比對。
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if logSortKey(names[j]) < logSortKey(names[i]) {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("sorted[%d] = %q, want %q (full=%v)", i, names[i], want[i], names)
		}
	}
}

func TestLogSortKey_DesignReviewerOrder(t *testing.T) {
	names := []string{
		"round-2-acceptor.log",
		"round-2-design-reviewer.log",
		"round-2-deep-reviewer-1.log",
		"round-2-deep-reviewer-2.log",
		"round-2-deep-reviewer-3.log",
		"round-2-synthesizer.log",
	}
	want := []string{
		"round-2-design-reviewer.log",
		"round-2-deep-reviewer-1.log",
		"round-2-deep-reviewer-2.log",
		"round-2-deep-reviewer-3.log",
		"round-2-synthesizer.log",
		"round-2-acceptor.log",
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if logSortKey(names[j]) < logSortKey(names[i]) {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("sorted[%d] = %q, want %q (full=%v)", i, names[i], want[i], names)
		}
	}
}

func TestFindActiveLogs_MultipleConcurrent(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	write := func(name string, modAgo time.Duration) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := now.Add(-modAgo)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	// 兩個平行 sub-reviewer 幾乎同時寫入（落在 activeLogWindow 內）；舊 log 在窗外。
	write("round-1-deep-reviewer-1.log", 1*time.Second)
	write("round-1-deep-reviewer-2.log", 2*time.Second)
	write("round-1-coder.log", 5*time.Minute)
	write("notes.txt", 0)

	got := findActiveLogs(dir, nil)
	if len(got) != 2 {
		t.Fatalf("got %v, want 2 active logs", got)
	}
	// 依 logSortKey 排序：reviewer-1 在 reviewer-2 之前；舊 coder log 不入列。
	if got[0] != "round-1-deep-reviewer-1.log" || got[1] != "round-1-deep-reviewer-2.log" {
		t.Errorf("got %v, want [reviewer-1, reviewer-2]", got)
	}
}

func TestFindActiveLogs_EmptyDir(t *testing.T) {
	if got := findActiveLogs(t.TempDir(), nil); got != nil {
		t.Errorf("got %v, want nil for empty dir", got)
	}
}
