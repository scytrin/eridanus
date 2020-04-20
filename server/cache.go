package server

import (
	"sync"

	"go.chromium.org/luci/common/data/stringset"
)

var gslCache sync.RWMutex

type Cache map[string]stringset.Set // map of a hex hash to set of tags

func (m Cache) Len() int {
	gslCache.RLock()
	defer gslCache.RUnlock()

	return len(m)
}

func (m Cache) GetTags(idHash string) ([]string, error) {
	gslCache.RLock()
	defer gslCache.RUnlock()

	if tagSet, ok := m[idHash]; ok {
		return tagSet.ToSlice(), nil
	}

	return nil, nil
}

func (m Cache) AddTags(idHash string, tags ...string) error {
	gslCache.Lock()
	defer gslCache.Unlock()

	if m == nil {
		m = make(Cache)
	}

	if _, ok := m[idHash]; !ok {
		m[idHash] = stringset.NewFromSlice("system:new")
	}

	// log.Infof("Adding to %s:\n%s", idHash, strings.Join(tags, "\n"))
	m[idHash].AddAll(tags)

	return nil
}

func (m Cache) DelTags(idHash string, tags ...string) error {
	gslCache.Lock()
	defer gslCache.Unlock()

	if m == nil {
		m = make(Cache)
	}

	if _, ok := m[idHash]; !ok {
		m[idHash] = stringset.NewFromSlice("system:new")
	}

	// log.Infof("Removing from %s:\n%s", idHash, strings.Join(tags, "\n"))
	m[idHash].DelAll(tags)

	return nil
}

func (m Cache) Range(op func(string, []string) error) error {
	gslCache.RLock()
	defer gslCache.RUnlock()

	for idHash, tagSet := range m {
		if err := op(idHash, tagSet.ToSlice()); err != nil {
			return err
		}
	}

	return nil
}
