package eridanus

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestIDHashString(t *testing.T) {
	s := "test"
	v := IDHash(s).String()
	if s != v {
		t.Errorf("IDHash(s).String(): got %q, want %q", v, s)
	}
}

func TestIDHashHexColor(t *testing.T) {
	s := "abcdef0123456789"
	c := "#" + s[:6]
	h := IDHash(s)
	r := h.HexColor()
	if r != c {
		t.Errorf("IDHash(s).HexColor(): got %q, want %q", r, c)
	}
}

func TestIDHashHexColor_BadHash(t *testing.T) {
	s := "gggggg"
	c := ""
	h := IDHash(s)
	r := h.HexColor()
	if r != c {
		t.Errorf("IDHash(s).HexColor(): got %q, want %q", r, c)
	}
}

func TestIDHashesToSlice(t *testing.T) {
	s0 := []string{"a", "b", "c"}
	var hs IDHashes
	for _, e := range s0 {
		hs = append(hs, IDHash(e))
	}
	s1 := hs.ToSlice()
	if len(s0) != len(s1) {
		t.Fatalf("len(s0) != len(s1): %d %d", len(s0), len(s1))
	}
	for i, e := range hs.ToSlice() {
		if s0[i] != e {
			t.Errorf("hs[%d]: got %q, want %q", i, e, s0[i])
		}
	}
}

func TestTagString(t *testing.T) {
	s := "test"
	v := Tag(s).String()
	if s != v {
		t.Errorf("Tag(s).String(): got %q, want %q", v, s)
	}
}

func TestTagsTagsFromString(t *testing.T) {
	for i, test := range []struct {
		o string
		r []string
	}{
		{"a,b,c", []string{"a", "b", "c"}},
		{"c,b,a", []string{"a", "b", "c"}},
		{"d,ef,g,g,h", []string{"d", "ef", "g", "h"}},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			tags := TagsFromString(test.o)
			for j, v := range tags {
				if test.r[j] != v.String() {
					t.Errorf("tags[%d]: got %q, want %q", j, v, test.r[j])
				}
			}
		})
	}
}

func TestTagsOmitDuplicates(t *testing.T) {
	var hs Tags
	for _, e := range []string{"a", "b", "c", "c", "c"} {
		hs = append(hs, Tag(e))
	}
	hs = hs.OmitDuplicates()

	ts := make(map[Tag]int)
	for _, e := range hs {
		ts[e]++
	}
	for k, v := range ts {
		if v != 1 {
			t.Errorf("multiple values of %q: %d", k, v)
		}
	}
}

func TestTagsToSlice(t *testing.T) {
	s0 := []string{"a", "b", "c"}
	var hs Tags
	for _, e := range s0 {
		hs = append(hs, Tag(e))
	}
	for i, e := range hs.ToSlice() {
		if e != s0[i] {
			t.Errorf("hs[%d]: got %q, want %q", i, e, s0[i])
		}
	}
}

func TestTagsString(t *testing.T) {
	s0 := []string{"a", "b", "c"}
	var hs Tags
	for _, e := range s0 {
		hs = append(hs, Tag(e))
	}
	r0 := hs.String()
	r1 := strings.Join(s0, ",")
	if r0 != r1 {
		t.Errorf("hs.String(): got %v, want %v", r0, r1)
	}
}

func TestRecoveryHandler(t *testing.T) {
	for i, test := range []struct {
		err    interface{}
		errStr string
	}{
		{"test", "test"},
		{errors.New("test"), "test"},
		{&Command{}, fmt.Sprintf("panicked: %v", &Command{})},
		{nil, fmt.Sprintf("panicked: %v", nil)},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if err := func() (err error) {
				defer RecoveryHandler(func(rerr error) { err = rerr })
				panic(test.err)
			}(); err.Error() != test.errStr {
				t.Errorf("RecoveryHandler: got %v, want %v", err.Error(), test.errStr)
			}
		})
	}
}
