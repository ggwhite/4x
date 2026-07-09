// Package docscheck 存放對 repo 根目錄 dev 腳本（scripts/check-docs-sync.sh、
// scripts/check-guide-i18n.sh）的整合測試。此 package 不含 production 程式碼，
// 僅以 os/exec 在暫存 git repo 內執行「真實」腳本，斷言其 stdout/stderr/exit code。
package docscheck
