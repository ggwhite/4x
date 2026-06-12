package main

import (
	"encoding/json"
	"fmt"
	"os"
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
