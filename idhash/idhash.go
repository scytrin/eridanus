package idhash

import (
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/scytrin/eridanus"
)

// GenerateIDHash returns a hashsum that will be used to identify the content.
func GenerateIDHash(r io.Reader) (eridanus.IDHash, error) {
	h := sha256.New()
	io.Copy(h, r)
	return eridanus.IDHash(fmt.Sprintf("%x", h.Sum(nil))), nil
}
