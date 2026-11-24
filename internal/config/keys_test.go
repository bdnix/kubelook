package config_test

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bdnix/kubelook/internal/config"
)

// keysOf lists the dotted keys of the configuration file: the leaves of the structure, with a slice or a map of
// plain values counted as one key and the module switches written as modules.<name>.enabled.
func keysOf(t reflect.Type, prefix string) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" || (name == "" && !f.Anonymous) {
			continue
		}
		key := prefix
		if name != "" {
			key = prefix + name
		}
		switch {
		case f.Anonymous && name == "": // an embedded struct (Toggle) is flattened
			out = append(out, keysOf(f.Type, prefix)...)
		case f.Type.Kind() == reflect.Map && f.Type.Elem() == reflect.TypeOf(config.Module{}):
			out = append(out, key+".<name>.enabled")
		case f.Type.Kind() == reflect.Struct && f.Type != reflect.TypeOf(config.Duration(0)):
			out = append(out, keysOf(f.Type, key+".")...)
		default:
			out = append(out, key)
		}
	}
	return out
}

// TestPrintKeys prints the key list that hack/docs-check.sh compares with docs/configuration.md:
// go test ./internal/config -run TestPrintKeys -v
func TestPrintKeys(t *testing.T) {
	keys := keysOf(reflect.TypeOf(config.Config{}), "")
	sort.Strings(keys)
	for _, k := range keys {
		t.Log("KEY " + k)
	}

	// The list is not empty by accident: these keys exist, and none of the secrets or internals is in it.
	for _, want := range []string{"server.publicURL", "vault.candidateRoles", "fileBrowser.enabled", "fileBrowser.maxTransferBytes",
		"modules.<name>.enabled", "prometheus.clusters", "debug.profiles", "helm.repositories", "egress.blockLocalAddresses"} {
		assert.Contains(t, keys, want)
	}
	assert.NotContains(t, keys, "EncryptionKey")
	assert.Len(t, keys, 47, "a key was added or removed: update docs/configuration.md and this number")
}
