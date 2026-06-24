package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/gitops"
	"github.com/ggwhite/4x/internal/protocol"
)

func dryRunLoop(ws *protocol.Workspace, feature feat.Feature, cfg protocol.Config, s protocol.State) error {
	phases := []struct {
		phase protocol.Phase
		role  protocol.Role
	}{
		{protocol.PhaseDesigning, protocol.RoleDesigner},
		{protocol.PhaseCoding, protocol.RoleCoder},
		{protocol.PhaseReviewing, protocol.RoleReviewer},
		{protocol.PhaseTesting, protocol.RoleTester},
		{protocol.PhaseDeepReviewing, protocol.RoleDeepReviewer},
		{protocol.PhaseAccepting, protocol.RoleAcceptor},
	}

	for _, p := range phases {
		fmt.Printf("=== %s (%s) ===\n", p.phase, p.role)
		prompt, err := generatePrompt(ws, ws, feature, cfg, p.role, 1, 0)
		if err != nil {
			fmt.Printf("  (error: %v)\n\n", err)
			continue
		}
		fmt.Println(prompt)
		fmt.Println()
	}
	return nil
}

// syncFeatureToWorktree 將主 workspace 的 feature 目錄複製到 worktree，
// 確保 runner 能讀到最新的 protocol 檔案（task-brief、上一輪 report 等）
func syncFeatureToWorktree(main, wt *protocol.Workspace, featureID string, round int) {
	srcDir := main.FeatureDir(featureID)
	dstDir := wt.FeatureDir(featureID)
	os.MkdirAll(dstDir, 0o755)

	// feature YAML（.4x/features/{id}.yaml）— runner 需要它來跑 `4x verify`
	srcYAML := filepath.Join(main.DotDir(), protocol.FeaturesDir, featureID+".yaml")
	dstFeaturesDir := filepath.Join(wt.DotDir(), protocol.FeaturesDir)
	os.MkdirAll(dstFeaturesDir, 0o755)
	gitops.CopyFileIfExists(srcYAML, filepath.Join(dstFeaturesDir, featureID+".yaml"))

	// state + feature-level 檔案
	// 帶入 SelectedLearningsFile，讓 resume 重建 worktree 時 Designer 先前的選擇不致遺失。
	for _, name := range []string{protocol.StateFile, protocol.TaskBrief, protocol.Criteria, protocol.TestStratFile, protocol.DesignReviewReport, protocol.SelectedLearningsFile} {
		gitops.CopyFileIfExists(filepath.Join(srcDir, name), filepath.Join(dstDir, name))
	}

	// 當前 round 目錄
	if round > 0 {
		srcRound := main.RoundDir(featureID, round)
		dstRound := wt.RoundDir(featureID, round)
		os.MkdirAll(dstRound, 0o755)
		entries, _ := os.ReadDir(srcRound)
		for _, e := range entries {
			if !e.IsDir() {
				gitops.CopyFileIfExists(filepath.Join(srcRound, e.Name()), filepath.Join(dstRound, e.Name()))
			}
		}
	}
}

// syncFeatureFromWorktree 將 worktree 裡 runner 寫的 protocol 檔案複製回主 workspace。
// 回傳彙整後的 error（任一 MkdirAll / ReadDir / CopyFileIfExists 失敗），讓 caller 能在
// disk full 等情況印出真因，而非只看到下游的 missing-artifact。來源檔不存在不算 error。
func syncFeatureFromWorktree(wt, main *protocol.Workspace, featureID string, round int) error {
	srcDir := wt.FeatureDir(featureID)
	dstDir := main.FeatureDir(featureID)
	var errs []string
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		errs = append(errs, err.Error())
	}

	// feature-level 檔案
	// SelectedLearningsFile（Designer 選出）與 RetroLearningsFile（Acceptor 產出）是 feature 層
	// runner artifact，後續 role 的 prompt 注入、updateLearningsUsage 與 harvestLearnings 都從主
	// workspace 讀取，必須在 worktree 模式下隨此 sync 帶回，否則 learnings 注入迴路會靜默失效。
	for _, name := range []string{
		protocol.TaskBrief, protocol.Criteria, protocol.TestStratFile,
		protocol.DesignReviewReport, protocol.FinalReport,
		protocol.SelectedLearningsFile, protocol.RetroLearningsFile,
	} {
		if _, err := gitops.CopyFileIfNewer(filepath.Join(srcDir, name), filepath.Join(dstDir, name)); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}

	// round 目錄
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
		if !e.IsDir() {
			if _, err := gitops.CopyFileIfNewer(filepath.Join(srcRound, e.Name()), filepath.Join(dstRound, e.Name())); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", e.Name(), err))
			}
		}
	}

	// e2e/screenshots 目錄（tester 截圖）
	srcE2E := filepath.Join(srcDir, "e2e", "screenshots")
	dstE2E := filepath.Join(dstDir, "e2e", "screenshots")
	if info, err := os.Stat(srcE2E); err == nil && info.IsDir() {
		roundDirs, _ := os.ReadDir(srcE2E)
		for _, rd := range roundDirs {
			if !rd.IsDir() {
				continue
			}
			srcRoundScreens := filepath.Join(srcE2E, rd.Name())
			dstRoundScreens := filepath.Join(dstE2E, rd.Name())
			if err := os.MkdirAll(dstRoundScreens, 0o755); err != nil {
				errs = append(errs, err.Error())
				continue
			}
			files, _ := os.ReadDir(srcRoundScreens)
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				if _, err := gitops.CopyFileIfNewer(filepath.Join(srcRoundScreens, f.Name()), filepath.Join(dstRoundScreens, f.Name())); err != nil {
					errs = append(errs, fmt.Sprintf("screenshot %s/%s: %v", rd.Name(), f.Name(), err))
				}
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("sync from worktree: %s", strings.Join(errs, "; "))
	}
	return nil
}

// startLiveSync 啟動背景 goroutine，每 2 秒將 worktree 的 protocol 檔案同步回 main workspace。
// 回傳的 stop function 為阻塞式：close(done) 後 wg.Wait() 確保 in-flight 的 sync 完成才返回，
// 避免 caller 隨即執行的 final sync 與背景 sync 競爭寫同一批檔案。
func startLiveSync(wt, main *protocol.Workspace, featureID string, round int) func() {
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
				if err := syncFeatureFromWorktree(wt, main, featureID, round); err != nil {
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
