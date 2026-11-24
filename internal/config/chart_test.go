package config_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bdnix/kubelook/internal/config"
)

// TestLoad_RenderedChartConfig loads the config.yaml that the Helm chart renders from ci/full-values.yaml
// (hack/chart-test.sh sets KUBELOOK_TEST_CHART_CONFIG) and checks that it loads to the same configuration as
// testdata/full.yaml, which holds the same non-default value for every setting. It is skipped when the variable
// is unset.
func TestLoad_RenderedChartConfig(t *testing.T) {
	path := os.Getenv("KUBELOOK_TEST_CHART_CONFIG")
	if path == "" {
		t.Skip("KUBELOOK_TEST_CHART_CONFIG is not set: run hack/chart-test.sh")
	}
	getenv := env(map[string]string{
		"KUBELOOK_DATABASE_URL":         "postgres://app@db/kubelook",
		"KUBELOOK_GOOGLE_CLIENT_SECRET": "g",
		"KUBELOOK_GITHUB_CLIENT_SECRET": "h",
	})

	want, err := config.Load("testdata/full.yaml", getenv)
	require.NoError(t, err)
	got, err := config.Load(path, getenv)
	require.NoError(t, err, "the rendered configuration must load with the strict loader")

	require.Equal(t, want, got)
}
