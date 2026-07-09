package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserConfigDashboardAuthRoundTrip(t *testing.T) {
	t.Run("explicit false round-trips", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		fal := false
		if err := WriteUserConfig(UserConfig{DashboardAuth: &fal}); err != nil {
			t.Fatalf("WriteUserConfig: %v", err)
		}
		got, err := ReadUserConfig()
		if err != nil {
			t.Fatalf("ReadUserConfig: %v", err)
		}
		if got.DashboardAuth == nil {
			t.Fatal("DashboardAuth = nil, want non-nil pointer to false")
		}
		if *got.DashboardAuth != false {
			t.Fatalf("*DashboardAuth = %v, want false", *got.DashboardAuth)
		}
	})

	t.Run("nil omits key in json", func(t *testing.T) {
		data, err := json.Marshal(UserConfig{})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "dashboard_auth") {
			t.Fatalf("nil DashboardAuth should be omitted, got %s", data)
		}
	})
}
