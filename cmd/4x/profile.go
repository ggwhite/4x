package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	feat "github.com/ggwhite/4x/internal/feature"
	"github.com/ggwhite/4x/internal/protocol"
)

// isInteractiveTerminal 回報 stdin 與 stdout 是否皆為互動式終端機（char device）。
func isInteractiveTerminal() bool {
	return isCharDevice(os.Stdin) && isCharDevice(os.Stdout)
}

func isCharDevice(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// profileOptions 回傳可選 profile 名稱清單（cfg.Profiles ∪ DefaultProfiles），
// 依 canonical 順序（full/normal/quick）排在前、其餘自訂 profile 字母序在後，供互動選單列舉。
func profileOptions(cfg protocol.Config) []string {
	seen := map[string]bool{}
	var ordered []string
	for _, name := range []string{"full", "normal", "quick"} {
		if _, ok := protocol.DefaultProfiles()[name]; ok {
			ordered = append(ordered, name)
			seen[name] = true
		}
	}
	var custom []string
	for name := range cfg.Profiles {
		if !seen[name] {
			custom = append(custom, name)
			seen[name] = true
		}
	}
	sort.Strings(custom)
	return append(ordered, custom...)
}

// selectProfileInteractive 用互動式選單讓使用者選取 pipeline profile，支援上下鍵導航。
// 預設項為 cfg.DefaultProfile（未設定時為 full）。回傳選定的 profile 名稱。
func selectProfileInteractive(_ io.Reader, _ io.Writer, cfg protocol.Config, feature feat.Feature) (string, error) {
	options := profileOptions(cfg)
	if len(options) == 0 {
		return "", nil
	}
	def := cfg.DefaultProfile
	if def == "" {
		def = "full"
	}

	defaults := protocol.DefaultProfiles()
	lookupProfile := func(name string) protocol.ProfileConfig {
		if pc, ok := cfg.Profiles[name]; ok {
			return pc
		}
		if pc, ok := defaults[name]; ok {
			return pc
		}
		return protocol.ProfileConfig{}
	}

	huhOptions := make([]huh.Option[string], 0, len(options))
	for _, name := range options {
		pc := lookupProfile(name)
		phases := make([]string, 0, len(pc.Phases))
		for _, ps := range pc.Phases {
			phases = append(phases, ps.Phase)
		}
		label := fmt.Sprintf("%s  [%s]", name, strings.Join(phases, " → "))
		huhOptions = append(huhOptions, huh.NewOption(label, name))
	}

	var selected string
	km := huh.NewDefaultKeyMap()
	km.Quit.SetKeys("ctrl+c", "esc")
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(fmt.Sprintf("Select pipeline profile for %s", feature.ID)).
				Options(huhOptions...).
				Value(&selected),
		),
	).WithKeyMap(km).Run()
	if err != nil {
		return "", fmt.Errorf("profile selection cancelled")
	}
	return selected, nil
}
