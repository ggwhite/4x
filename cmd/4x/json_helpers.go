package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// jsonError prints the error message as JSON to stdout and exits with code 1.
func jsonError(msg string) error {
	result := struct {
		Error string `json:"error"`
	}{
		Error: msg,
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(data))
	os.Exit(1)
	return nil
}

// withJsonError 包裝 cobra RunE，在最外層統一處理 jsonOutput 的 error 格式化。
// 當 inner function 回傳非 nil error 且 jsonFlag 為 true 時，改以 JSON 結構輸出錯誤
// （沿用 jsonError，含 os.Exit(1) 以維持 exit code 1 與乾淨的 JSON-only 輸出）；
// 否則原樣回傳 error，交由 Cobra 走既有純文字錯誤路徑。
// 用途：讓各 command 的 RunE 內部直接 return err，不必每個 error path 各寫一次
// jsonOutput 判斷。
func withJsonError(jsonFlag *bool, fn func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := fn(cmd, args)
		if err != nil && *jsonFlag {
			return jsonError(err.Error())
		}
		return err
	}
}
