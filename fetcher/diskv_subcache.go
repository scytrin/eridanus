package fetcher

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/scytrin/eridanus"
	"github.com/sirupsen/logrus"
)

var webcacheNamespace = "web_cache2"

type storageCache struct {
	s eridanus.Storage
}

func (c *storageCache) keyTransform(key string) string {
	logrus.Debug(key)
	// bad := []string{`/`, `\`, `?`, `%`, `*`, `:`, `|`, `"`, `<`, `>`, `.`, ` `}
	// for _, v := range bad {
	// 	key = strings.ReplaceAll(key, v, "_")
	// }
	// logrus.Info(key)

	key = fmt.Sprintf("%x", md5.Sum([]byte(key)))
	return fmt.Sprintf("%s/%s", webcacheNamespace, key)
}

func (c *storageCache) Get(key string) ([]byte, bool) {
	cPath := c.keyTransform(key)
	rc, err := c.s.Get(cPath)
	if err != nil {
		if !os.IsNotExist(err) {
			logrus.Error(err)
		}
		return nil, false
	}
	defer rc.Close()
	data, err := ioutil.ReadAll(rc)
	if err != nil {
		logrus.Error(err)
		return nil, false
	}
	return data, true
}

func (c *storageCache) Set(key string, data []byte) {
	cPath := c.keyTransform(key)
	if err := c.s.Set(cPath, bytes.NewReader(data)); err != nil {
		logrus.Error(err)
	}
}

func (c *storageCache) Delete(key string) {
	cPath := c.keyTransform(key)
	if err := c.s.Delete(cPath); err != nil && !os.IsNotExist(err) {
		logrus.Error(err)
	}
}
