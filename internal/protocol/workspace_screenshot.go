package protocol

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ggwhite/4x/internal/feature"
)

// DiscoverScreenshots 會合併 verify.json 與截圖目錄掃描結果，並按 round 分組回傳。
func (w *Workspace) DiscoverScreenshots(featureID, screenshotDir string) ([]feature.ScreenshotGroup, error) {
	if screenshotDir == "" {
		screenshotDir = feature.DefaultScreenshotDir
	}

	byRound := make(map[int][]feature.Screenshot)
	seenPath := make(map[string]struct{})

	rounds, err := w.discoverFromVerify(featureID, byRound, seenPath)
	if err != nil {
		return nil, err
	}
	if err := w.discoverFromDir(featureID, screenshotDir, rounds, byRound, seenPath); err != nil {
		return nil, err
	}
	if wtRoot := w.WorktreePath(featureID); wtRoot != "" && wtRoot != w.Root {
		w.discoverFromWorktree(wtRoot, featureID, screenshotDir, rounds, byRound, seenPath)
	}

	keys := make([]int, 0, len(byRound))
	for round, shots := range byRound {
		if len(shots) == 0 {
			continue
		}
		feature.SortScreenshots(shots)
		byRound[round] = shots
		keys = append(keys, round)
	}
	sort.Ints(keys)

	groups := make([]feature.ScreenshotGroup, 0, len(keys))
	for _, round := range keys {
		groups = append(groups, feature.ScreenshotGroup{Round: round, Screenshots: byRound[round]})
	}
	return groups, nil
}

func (w *Workspace) discoverFromVerify(
	featureID string,
	byRound map[int][]feature.Screenshot,
	seenPath map[string]struct{},
) ([]int, error) {
	roundsDir := filepath.Join(w.FeatureDir(featureID), RoundsDir)
	entries, err := os.ReadDir(roundsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read rounds dir: %w", err)
	}

	roundSet := make(map[int]struct{})
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "round-") {
			continue
		}
		roundNum, err := strconv.Atoi(strings.TrimPrefix(e.Name(), "round-"))
		if err != nil || roundNum <= 0 {
			continue
		}
		roundSet[roundNum] = struct{}{}

		verifyPath := filepath.Join(roundsDir, e.Name(), VerifyFile)
		data, err := os.ReadFile(verifyPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", verifyPath, err)
		}

		var evidence VerifyEvidence
		if err := json.Unmarshal(data, &evidence); err != nil {
			return nil, fmt.Errorf("parse %s: %w", verifyPath, err)
		}
		for _, raw := range evidence.Screenshots {
			path := feature.NormalizeScreenshotPath(raw.Path)
			if path == "" {
				continue
			}
			if strings.HasPrefix(path, "../") || path == ".." {
				continue
			}
			if !feature.IsScreenshotFile(filepath.Base(path)) {
				continue
			}
			if _, ok := seenPath[path]; ok {
				continue
			}
			shot := raw
			shot.Path = path
			if shot.Step == "" || shot.Description == "" {
				step, desc := feature.ParseScreenshotFilename(filepath.Base(path))
				if shot.Step == "" {
					shot.Step = step
				}
				if shot.Description == "" {
					shot.Description = desc
				}
			}

			byRound[roundNum] = append(byRound[roundNum], shot)
			seenPath[path] = struct{}{}
		}
	}

	rounds := make([]int, 0, len(roundSet))
	for round := range roundSet {
		rounds = append(rounds, round)
	}
	sort.Ints(rounds)
	return rounds, nil
}

func (w *Workspace) discoverFromDir(
	featureID, screenshotDir string,
	rounds []int,
	byRound map[int][]feature.Screenshot,
	seenPath map[string]struct{},
) error {
	targets := resolveScreenshotDirs(w.Root, featureID, screenshotDir, rounds)
	for _, target := range targets {
		dirPath := target.Dir
		if !filepath.IsAbs(dirPath) {
			dirPath = filepath.Join(w.Root, dirPath)
		}
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read screenshot dir %s: %w", dirPath, err)
		}

		for _, e := range entries {
			if e.IsDir() || !feature.IsScreenshotFile(e.Name()) {
				continue
			}
			absPath := filepath.Join(dirPath, e.Name())
			rel, err := filepath.Rel(w.Root, absPath)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			if strings.HasPrefix(rel, "../") || rel == ".." {
				continue
			}
			rel = feature.NormalizeScreenshotPath(rel)
			if _, ok := seenPath[rel]; ok {
				continue
			}
			step, desc := feature.ParseScreenshotFilename(e.Name())
			shot := feature.Screenshot{Path: rel, Step: step, Description: desc}
			byRound[target.Round] = append(byRound[target.Round], shot)
			seenPath[rel] = struct{}{}
		}
	}
	return nil
}

type screenshotScanTarget struct {
	Round int
	Dir   string
}

func resolveScreenshotDirs(workspaceRoot, featureID, screenshotDir string, rounds []int) []screenshotScanTarget {
	template := strings.ReplaceAll(screenshotDir, "{feature-id}", featureID)
	if strings.Contains(template, "{round}") {
		roundSet := make(map[int]struct{})
		for _, r := range rounds {
			if r > 0 {
				roundSet[r] = struct{}{}
			}
		}
		for _, r := range discoverRoundsFromTemplate(workspaceRoot, template) {
			roundSet[r] = struct{}{}
		}
		candidates := make([]int, 0, len(roundSet))
		for r := range roundSet {
			candidates = append(candidates, r)
		}
		if len(candidates) == 0 {
			candidates = []int{1}
		}
		sort.Ints(candidates)
		targets := make([]screenshotScanTarget, 0, len(candidates))
		for _, round := range candidates {
			dir := strings.ReplaceAll(template, "{round}", strconv.Itoa(round))
			targets = append(targets, screenshotScanTarget{Round: round, Dir: dir})
		}
		return targets
	}
	round := 1
	if len(rounds) > 0 {
		round = rounds[len(rounds)-1]
	}
	return []screenshotScanTarget{{Round: round, Dir: template}}
}

func discoverRoundsFromTemplate(workspaceRoot, template string) []int {
	absTemplate := strings.TrimSuffix(filepath.ToSlash(template), "/")
	if !filepath.IsAbs(absTemplate) {
		absTemplate = filepath.ToSlash(filepath.Join(workspaceRoot, absTemplate))
	}
	idx := strings.Index(absTemplate, "{round}")
	if idx < 0 {
		return nil
	}

	pattern := strings.ReplaceAll(absTemplate, "{round}", "*")
	matches, err := filepath.Glob(filepath.FromSlash(pattern))
	if err != nil {
		return nil
	}

	prefix := absTemplate[:idx]
	suffix := absTemplate[idx+len("{round}"):]
	roundSet := make(map[int]struct{})
	for _, match := range matches {
		candidate := strings.TrimSuffix(filepath.ToSlash(match), "/")
		if !strings.HasPrefix(candidate, prefix) || !strings.HasSuffix(candidate, suffix) {
			continue
		}
		middle := strings.TrimSuffix(strings.TrimPrefix(candidate, prefix), suffix)
		if round, err := strconv.Atoi(middle); err == nil && round > 0 {
			roundSet[round] = struct{}{}
		}
	}
	if len(roundSet) == 0 {
		return nil
	}

	rounds := make([]int, 0, len(roundSet))
	for round := range roundSet {
		rounds = append(rounds, round)
	}
	sort.Ints(rounds)
	return rounds
}

func (w *Workspace) discoverFromWorktree(
	wtRoot, featureID, screenshotDir string,
	rounds []int,
	byRound map[int][]feature.Screenshot,
	seenPath map[string]struct{},
) {
	targets := resolveScreenshotDirs(wtRoot, featureID, screenshotDir, rounds)
	for _, target := range targets {
		dirPath := target.Dir
		if !filepath.IsAbs(dirPath) {
			dirPath = filepath.Join(wtRoot, dirPath)
		}
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !feature.IsScreenshotFile(e.Name()) {
				continue
			}
			absPath := filepath.Join(dirPath, e.Name())
			rel, err := filepath.Rel(w.Root, absPath)
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			rel = filepath.ToSlash(rel)
			normalized := feature.NormalizeScreenshotPath(rel)
			if _, seen := seenPath[normalized]; seen {
				continue
			}
			seenPath[normalized] = struct{}{}
			step, desc := feature.ParseScreenshotFilename(e.Name())
			byRound[target.Round] = append(byRound[target.Round], feature.Screenshot{
				Path:        rel,
				Step:        step,
				Description: desc,
			})
		}
	}
}

// ScreenshotDir 從 config 取得 tester 的截圖目錄，fallback 到 feature.DefaultScreenshotDir。
func ScreenshotDir(cfg Config) string {
	if tester, ok := cfg.Roles[string(RoleTester)]; ok && strings.TrimSpace(tester.ScreenshotDir) != "" {
		return tester.ScreenshotDir
	}
	return feature.DefaultScreenshotDir
}
