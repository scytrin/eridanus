package idhash

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/scytrin/eridanus"
	"github.com/sirupsen/logrus"
)

// type IDHashStr string

// GenerateIDHash returns a hashsum that will be used to identify the content.
func GenerateIDHash(r io.Reader) (eridanus.IDHash, error) {
	h := sha256.New()
	io.Copy(h, r)
	return eridanus.IDHash(fmt.Sprintf("%x", h.Sum(nil))), nil
}

// HashToHexColor returns a value acceptable to use in specifying color.
func HashToHexColor(idHash eridanus.IDHash) string {
	c, err := hex.DecodeString(string(idHash))
	if err != nil {
		logrus.Error(err)
		return ""
	}
	return hex.EncodeToString(c[:3])
}
