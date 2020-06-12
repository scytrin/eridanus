package idhash

import (
	"crypto/sha256"
	"io"
	"math/big"
	"fmt"
)

// type IDHashStr string

// IDHash returns a hashsum that will be used to identify the content.
func IDHash(r io.Reader) (string, error) {
	h := sha256.New()
	io.Copy(h, r)
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// HashToHexColor returns a value acceptable to use in specifying color.
func HashToHexColor(idHash string) string {
	i := big.NewInt(0)
	if _, ok := i.SetString(idHash, 16); !ok {
		return ""
	}
	return i.Mod(i, big.NewInt(0xffffff)).Text(16)
}
