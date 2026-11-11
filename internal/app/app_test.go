package app_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bdnix/kubelook/internal/app"
)

func run(args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = app.Main(args, &out, &errb, func(string) string { return "" })
	return code, out.String(), errb.String()
}

func TestMain_Version_PrintsVersionAndReturns0(t *testing.T) {
	code, out, _ := run("version")
	assert.Equal(t, 0, code)
	assert.Equal(t, "kubelook dev (commit unknown)\n", out)
}

func TestMain_NoArgs_PrintsUsageAndReturns2(t *testing.T) {
	code, _, errOut := run()
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "usage: kubelook <serve|migrate|audit-cleanup|version>")
}

func TestMain_UnknownCommand_PrintsUsageAndReturns2(t *testing.T) {
	code, _, errOut := run("bogus")
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, `unknown command "bogus"`)
}
