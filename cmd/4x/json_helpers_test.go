package main

import (
	"fmt"
	"testing"

	"github.com/spf13/cobra"
)

func TestWithJsonError_NoError_ReturnsNil(t *testing.T) {
	jsonFlag := true
	wrapped := withJsonError(&jsonFlag, func(cmd *cobra.Command, args []string) error {
		return nil
	})
	if err := wrapped(nil, nil); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestWithJsonError_ErrorNoJson_PassesThrough(t *testing.T) {
	jsonFlag := false
	wrapped := withJsonError(&jsonFlag, func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("something broke")
	})
	err := wrapped(nil, nil)
	if err == nil || err.Error() != "something broke" {
		t.Errorf("expected 'something broke', got %v", err)
	}
}
