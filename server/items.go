package server

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"go.chromium.org/luci/common/data/stringset"
)

const (
	SQL_CreateItems = `CREATE TABLE IF NOT EXISTS items (
    hash TEXT PRIMARY KEY,
    tags TEXT NOT NULL
  ) WITHOUT ROWID;`
)

type Cache struct {
	sync.RWMutex
	stash map[string]stringset.Set // map of a hex hash to set of tags
}

func (c *Cache) Len() int {
	c.RLock()
	defer c.RUnlock()

	return len(c.stash)
}

func (c *Cache) GetTags(itemHash string) ([]string, error) {
	c.RLock()
	defer c.RUnlock()

	if tagSet, ok := c.stash[itemHash]; ok {
		return tagSet.ToSlice(), nil
	}

	return nil, nil
}

func (c *Cache) AddTags(itemHash string, tags ...string) error {
	c.Lock()
	defer c.Unlock()

	if c.stash == nil {
		c.stash = make(map[string]stringset.Set)
	}

	if _, ok := c.stash[itemHash]; !ok {
		c.stash[itemHash] = stringset.New(1)
		c.stash[itemHash].Add("system:new")
	}

	log.Infof("Adding to %s: %s", itemHash, tags)
	c.stash[itemHash].AddAll(tags)

	return nil
}

func (c *Cache) DelTags(itemHash string, tags ...string) error {
	c.Lock()
	defer c.Unlock()

	if c.stash == nil {
		c.stash = make(map[string]stringset.Set)
	}

	if _, ok := c.stash[itemHash]; !ok {
		c.stash[itemHash] = stringset.New(1)
		c.stash[itemHash].Add("system:new")
	}

	log.Infof("Removing from %s: %s", itemHash, tags)
	c.stash[itemHash].DelAll(tags)

	return nil
}

func (c *Cache) Range(op func(string, []string) error) error {
	c.RLock()
	defer c.RUnlock()

	for itemHash, tagSet := range c.stash {
		if err := op(itemHash, tagSet.ToSlice()); err != nil {
			return err
		}
	}

	return nil
}

var (
	fieldDelim  = ","
	recordDelim = "\n"
)

func (c *Cache) MarshalText() ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	for h, ts := range c.stash {
		buf.WriteString(h)
		for t := range ts {
			buf.WriteString(fieldDelim)
			buf.WriteString(t)
		}
		buf.WriteString(recordDelim)
	}
	return buf.Bytes(), nil
}

func (c *Cache) UnmarshalText(text []byte) error {
	newStash := make(map[string]stringset.Set)

	scanner := bufio.NewScanner(bytes.NewBuffer(text))
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), fieldDelim)
		newStash[parts[0]] = stringset.NewFromSlice(parts[1:]...)
	}
	if err := scanner.Err(); err != nil {
		log.Errorf("reading standard input: %v", err)
	}

	log.Infof("loaded %d entries", len(newStash))
	c.stash = newStash
	return nil
}

func (c *Cache) Load(path string) error {
	if path == "" {
		return nil
	}
	t, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	return c.UnmarshalText(t)
}

func (c *Cache) Save(path string) error {
	if path == "" {
		return nil
	}
	t, err := c.MarshalText()
	if err != nil {
		return err
	}
	log.Infof("serialized: %s", t)
	if err := ioutil.WriteFile(path, t, 0644); err != nil {
		return err
	}
	return nil
}
