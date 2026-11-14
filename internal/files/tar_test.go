package files_test

import (
	"archive/tar"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bdnix/kubelook/internal/files"
)

func TestValidateTarHeader(t *testing.T) {
	tests := []struct {
		name string
		h    tar.Header
		root string
		ok   bool
	}{
		{"plain file", tar.Header{Name: "site/a.txt", Typeflag: tar.TypeReg}, "site", true},
		{"the root itself", tar.Header{Name: "site/", Typeflag: tar.TypeDir}, "site", true},
		{"any root", tar.Header{Name: "whatever/a", Typeflag: tar.TypeReg}, "", true},
		{"absolute", tar.Header{Name: "/etc/passwd", Typeflag: tar.TypeReg}, "", false},
		{"dot dot element", tar.Header{Name: "a/../../b", Typeflag: tar.TypeReg}, "", false},
		{"dot dot in a name is fine", tar.Header{Name: "a/..b/c", Typeflag: tar.TypeReg}, "", true},
		{"outside the root", tar.Header{Name: "sitemap/a", Typeflag: tar.TypeReg}, "site", false},
		{"symlink inside", tar.Header{Name: "site/l", Typeflag: tar.TypeSymlink, Linkname: "a.txt"}, "site", true},
		{"symlink up but inside", tar.Header{Name: "site/d/l", Typeflag: tar.TypeSymlink, Linkname: "../a.txt"}, "site", true},
		{"symlink escaping", tar.Header{Name: "site/l", Typeflag: tar.TypeSymlink, Linkname: "../../x"}, "site", false},
		{"symlink absolute", tar.Header{Name: "site/l", Typeflag: tar.TypeSymlink, Linkname: "/x"}, "site", false},
		{"hardlink inside", tar.Header{Name: "site/h", Typeflag: tar.TypeLink, Linkname: "site/a.txt"}, "site", true},
		{"hardlink escaping", tar.Header{Name: "site/h", Typeflag: tar.TypeLink, Linkname: "../x"}, "site", false},
		{"hardlink absolute", tar.Header{Name: "site/h", Typeflag: tar.TypeLink, Linkname: "/x"}, "site", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := files.ValidateTarHeader(&tc.h, tc.root)
			if tc.ok {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, files.ErrUnsafe)
			}
		})
	}
}
