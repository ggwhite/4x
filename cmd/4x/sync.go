package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
	"github.com/ggwhite/4x/plugins"
	"github.com/spf13/cobra"
)

func newSyncCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync embedded plugin files to the project",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			ws, err := protocol.Find(cwd)
			if err != nil {
				return fmt.Errorf("找不到 .4x/ 目錄，請先執行 4x init")
			}

			cfg, err := ws.ReadConfig()
			if err != nil {
				return fmt.Errorf("read config: %w", err)
			}

			report := comparePlugins(ws.Root, cfg)

			if !dryRun {
				installPlugins(ws.Root, cfg)
			}

			if dryRun {
				fmt.Println("Dry run — no files written")
			}

			var updated, created, current int
			for _, r := range report {
				switch r.status {
				case statusCreated:
					fmt.Printf("  + %s (new)\n", r.path)
					created++
				case statusUpdated:
					fmt.Printf("  ~ %s (updated)\n", r.path)
					updated++
				case statusCurrent:
					fmt.Printf("    %s (current)\n", r.path)
					current++
				}
			}

			fmt.Printf("\n%d updated, %d created, %d already current\n", updated, created, current)
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Only report differences without writing files")
	return cmd
}

const (
	statusCreated = "created"
	statusUpdated = "updated"
	statusCurrent = "current"
)

type fileReport struct {
	path   string
	status string
}

func comparePlugins(root string, cfg protocol.Config) []fileReport {
	pluginDir := filepath.Join(root, ".4x", "plugins")
	var report []fileReport

	sharedFiles := []string{"shared/CREATOR.md"}
	for _, sf := range sharedFiles {
		embedData, err := plugins.FS.ReadFile(sf)
		if err != nil {
			continue
		}
		target := filepath.Join(pluginDir, sf)
		relPath := filepath.Join(".4x", "plugins", sf)
		existing, readErr := os.ReadFile(target)
		switch {
		case readErr != nil:
			report = append(report, fileReport{path: relPath, status: statusCreated})
		case !bytes.Equal(existing, embedData):
			report = append(report, fileReport{path: relPath, status: statusUpdated})
		default:
			report = append(report, fileReport{path: relPath, status: statusCurrent})
		}
	}

	for name := range cfg.Runners {
		installs := runnerInstalls(name)

		for _, d := range installs {
			embedData, err := plugins.FS.ReadFile(d.EmbedPath)
			if err != nil {
				continue
			}

			target := filepath.Join(pluginDir, d.PluginName)
			relPath := filepath.Join(".4x", "plugins", d.PluginName)

			existing, readErr := os.ReadFile(target)
			switch {
			case readErr != nil:
				report = append(report, fileReport{path: relPath, status: statusCreated})
			case !bytes.Equal(existing, embedData):
				report = append(report, fileReport{path: relPath, status: statusUpdated})
			default:
				report = append(report, fileReport{path: relPath, status: statusCurrent})
			}

			if d.RootFile != "" {
				importLine := "@.4x/plugins/" + d.PluginName
				rootTarget := filepath.Join(root, d.RootFile)
				rootData, readErr := os.ReadFile(rootTarget)
				switch {
				case readErr != nil:
					report = append(report, fileReport{path: d.RootFile, status: statusCreated})
				case !strings.Contains(string(rootData), importLine):
					report = append(report, fileReport{path: d.RootFile, status: statusUpdated})
				default:
					report = append(report, fileReport{path: d.RootFile, status: statusCurrent})
				}

				// installPlugins() 無條件對 CLAUDE.md 附加 learnings-context import（見 ensureAppendImport），
				// 不受 runner 是否啟用影響；但檔案不存在時 ensureAppendImport 不會建立它，故此處同樣略過。
				if d.RootFile == "CLAUDE.md" && readErr == nil {
					learningsImportLine := "@.4x/" + protocol.LearningsContextFile
					learningsPath := d.RootFile + " (learnings-context)"
					if strings.Contains(string(rootData), learningsImportLine) {
						report = append(report, fileReport{path: learningsPath, status: statusCurrent})
					} else {
						report = append(report, fileReport{path: learningsPath, status: statusUpdated})
					}
				}
			}
		}
	}

	sort.Slice(report, func(i, j int) bool {
		return report[i].path < report[j].path
	})
	return report
}
