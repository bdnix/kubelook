package version_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bdnix/kubelook/internal/version"
)

func TestString_DefaultValues_ReturnsDevUnknown(t *testing.T) {
	assert.Equal(t, "kubelook dev (commit unknown)", version.String())
}

func TestString_SetValues_ReturnsThem(t *testing.T) {
	oldV, oldC := version.Version, version.Commit
	t.Cleanup(func() { version.Version, version.Commit = oldV, oldC })
	version.Version, version.Commit = "1.2.3", "abc1234"
	assert.Equal(t, "kubelook 1.2.3 (commit abc1234)", version.String())
}
