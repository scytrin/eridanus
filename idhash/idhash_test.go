package idhash

import (
	"fmt"
	"strings"
	"testing"

	"github.com/scytrin/eridanus"
)

var (
	// value, hash, hexColor
	hashes = [][3]string{
		{"This is just a random string.",
			"94c22bf841b30ff895f075c8c8b8625539ef6f2ef2fd7ae196251d08e9db2a38",
			"#94c22b"},
		{"aaaa",
			"61be55a8e2f6b4e172338bddf184d6dbee29c98853e0a0485ecee7f27b9af0b4",
			"#61be55"},
		{"bbbb",
			"81cc5b17018674b401b42f35ba07bb79e211239c23bffe658da1577e3e646877",
			"#81cc5b"},
	}
)

func TestIDHash(t *testing.T) {
	for i, e := range hashes {
		v, h, c := e[0], e[1], e[2]
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			r := strings.NewReader(v)
			idhash, err := GenerateIDHash(r)
			if err != nil {
				t.Errorf("GenerateIDHash(r): got %v, want nil", err)
			}
			if idhash != eridanus.IDHash(h) {
				t.Errorf("GenerateIDHash(r): got %x, want %x", idhash, h)
			}
			color := idhash.HexColor()
			if color != c {
				t.Errorf("idhash.HexColor(): got %q, want %q", color, c)
			}
		})
	}
}
