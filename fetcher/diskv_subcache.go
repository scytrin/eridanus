package fetcher

import (
	"bytes"
	"io/ioutil"
	"os"

	"github.com/scytrin/eridanus"
	"github.com/sirupsen/logrus"
)

type storageCache struct {
	s eridanus.Storage
}

func (c *storageCache) Get(key string) ([]byte, bool) {
	wPath := URLToWebcachePath(key)
	rc, err := c.s.GetData(wPath)
	if err != nil {
		if logrus.IsLevelEnabled(logrus.DebugLevel) {
			logrus.Error(err)
		}
		return nil, false
	}
	defer rc.Close()
	wBytes, err := ioutil.ReadAll(rc)
	if err != nil {
		if logrus.IsLevelEnabled(logrus.DebugLevel) {
			logrus.Error(err)
		}
		return nil, false
	}
	return wBytes, true
}

func (c *storageCache) Set(key string, data []byte) {
	wPath := URLToWebcachePath(key)
	if err := c.s.PutData(wPath, bytes.NewReader(data)); err != nil {
		logrus.Error(err)
	}
}

func (c *storageCache) Delete(key string) {
	wPath := URLToWebcachePath(key)
	if err := c.s.DeleteData(wPath); err != nil && !os.IsNotExist(err) {
		logrus.Error(err)
	}
}
