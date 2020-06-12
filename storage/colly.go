package storage

import (
	"net/url"
	"sync"

	"github.com/gocolly/colly/v2/storage"
)

type collyStorage struct {
	sync.RWMutex
	*Storage
}

// CollyStorage returns an instance satisfying storage.Storage for gcolly.
func (s *Storage) CollyStorage() storage.Storage {
	return &collyStorage{Storage: s}
}

// Init satisfies storage.Storage.Init and queue.Storage.Init.
func (s *collyStorage) Init() error {
	return nil
}

// Visited satisfies storage.Storage.Visited.
func (s *collyStorage) Visited(requestID uint64) error {
	s.Lock()
	defer s.Unlock()
	s.collyVisited[requestID] = true
	return nil
}

// IsVisited satisfies storage.Storage.IsVisited.
func (s *collyStorage) IsVisited(requestID uint64) (bool, error) {
	s.RLock()
	defer s.RUnlock()
	return s.collyVisited[requestID], nil
}

// Cookies satisfies storage.Storage.Cookies.
func (s *collyStorage) Cookies(u *url.URL) string {
	s.RLock()
	defer s.RUnlock()
	return storage.StringifyCookies(s.cookieJar.Cookies(u))
}

// SetCookies satisfies storage.Storage.SetCookies.
func (s *collyStorage) SetCookies(u *url.URL, cookies string) {
	s.Lock()
	defer s.Unlock()
	s.cookieJar.SetCookies(u, storage.UnstringifyCookies(cookies))
}
