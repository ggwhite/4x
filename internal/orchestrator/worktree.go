package orchestrator

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
)

// SyncFeatureToWorktree 將主 workspace 的 feature 目錄複製到 worktree，
// 確保 runner 能讀到最新的 protocol 檔案（task-brief、上一輪 report 等）
func SyncFeatureToWorktree(main, wt *protocol.Workspace, featureID string, round int) {
	warn := func(err error, dst string) {
		if err != nil {
			slog.Warn("sync feature to worktree", "feature", featureID, "dst", dst, "err", err)
		}
	}

	srcDir := main.FeatureDir(featureID)
	dstDir := wt.FeatureDir(featureID)
	warn(os.MkdirAll(dstDir, 0o755), dstDir)

	srcYAML := filepath.Join(main.DotDir(), protocol.FeaturesDir, featureID+".yaml")
	dstFeaturesDir := filepath.Join(wt.DotDir(), protocol.FeaturesDir)
	warn(os.MkdirAll(dstFeaturesDir, 0o755), dstFeaturesDir)
	dstYAML := filepath.Join(dstFeaturesDir, featureID+".yaml")
	warn(gitops.CopyFileIfExists(srcYAML, dstYAML), dstYAML)

	// 帶入 SelectedLearningsFile，讓 resume 重建 worktree 時 Designer 先前的選擇不致遺失。
	for _, name := range []string{protocol.StateFile, protocol.TaskBrief, protocol.Criteria, protocol.TestStratFile, protocol.DesignReviewReport, protocol.SelectedLearningsFile} {
		dst := filepath.Join(dstDir, name)
		warn(gitops.CopyFileIfExists(filepath.Join(srcDir, name), dst), dst)
	}

	if round > 0 {
		srcRound := main.RoundDir(featureID, round)
		dstRound := wt.RoundDir(featureID, round)
		warn(os.MkdirAll(dstRound, 0o755), dstRound)
		entries, _ := os.ReadDir(srcRound)
		for _, e := range entries {
			if !e.IsDir() {
				dst := filepath.Join(dstRound, e.Name())
				warn(gitops.CopyFileIfExists(filepath.Join(srcRound, e.Name()), dst), dst)
			}
		}
	}
}

// SyncFeatureFromWorktree 將 worktree 裡 runner 寫的 protocol 檔案複製回主 workspace。
// 回傳彙整後的 error（任一 MkdirAll / ReadDir / CopyFileIfExists 失敗）。
func SyncFeatureFromWorktree(wt, main *protocol.Workspace, featureID string, round int) error {
	srcDir := wt.FeatureDir(featureID)
	dstDir := main.FeatureDir(featureID)
	var errs []string
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		errs = append(errs, err.Error())
	}

	for _, name := range []string{
		protocol.TaskBrief, protocol.Criteria, protocol.TestStratFile,
		protocol.DesignReviewReport, protocol.FinalReport,
		protocol.SelectedLearningsFile, protocol.RetroLearningsFile,
	} {
		if _, err := gitops.CopyFileIfNewer(filepath.Join(srcDir, name), filepath.Join(dstDir, name)); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}

	srcRound := wt.RoundDir(featureID, round)
	dstRound := main.RoundDir(featureID, round)
	if err := os.MkdirAll(dstRound, 0o755); err != nil {
		errs = append(errs, err.Error())
	}
	entries, err := os.ReadDir(srcRound)
	if err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Sprintf("read round dir: %v", err))
	}
	for _, e := range entries {
		src := filepath.Join(srcRound, e.Name())
		dst := filepath.Join(dstRound, e.Name())
		if e.IsDir() {
			// Agent 可能在 round 目錄下建立任意子目錄（例如截圖、附件），
			// 不能只複製頂層檔案而讓這些子目錄被靜默略過（見 round 收尾同步遺失截圖的事故）。
			if err := gitops.CopyDirIfNewer(src, dst); err != nil {
				errs = append(errs, fmt.Sprintf("%s/: %v", e.Name(), err))
			}
			continue
		}
		if _, err := gitops.CopyFileIfNewer(src, dst); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", e.Name(), err))
		}
	}

	srcE2E := filepath.Join(srcDir, "e2e", "screenshots")
	dstE2E := filepath.Join(dstDir, "e2e", "screenshots")
	if err := gitops.CopyDirIfNewer(srcE2E, dstE2E); err != nil {
		errs = append(errs, fmt.Sprintf("e2e/screenshots: %v", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("sync from worktree: %s", strings.Join(errs, "; "))
	}
	return nil
}

// StartLiveSync 啟動背景 goroutine，每 2 秒將 worktree 的 protocol 檔案同步回 main workspace。
// 回傳的 stop function 為阻塞式：close(done) 後 wg.Wait() 確保 in-flight 的 sync 完成才返回。
func StartLiveSync(wt, main *protocol.Workspace, featureID string, round int) func() {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := SyncFeatureFromWorktree(wt, main, featureID, round); err != nil {
					slog.Warn("live sync failed", "feature", featureID, "round", round, "error", err)
				}
			}
		}
	}()
	return func() {
		close(done)
		wg.Wait()
	}
}
