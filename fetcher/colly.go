package fetcher

import (
	"net/url"
	"sync"

	"github.com/gocolly/colly/v2/storage"
	"github.com/scytrin/eridanus"
)

type collyStorage struct {
	sync.RWMutex
	eridanus.Storage
	visited map[uint64]bool
}

// CollyStorage returns an instance satisfying storage.Storage for gcolly.
func NewCollyStorage(s eridanus.Storage) storage.Storage {
	return &collyStorage{Storage: s, visited: make(map[uint64]bool)}
}

// Init satisfies storage.Storage.Init and queue.Storage.Init.
func (s *collyStorage) Init() error {
	return nil
}

// Visited satisfies storage.Storage.Visited.
func (s *collyStorage) Visited(requestID uint64) error {
	s.Lock()
	defer s.Unlock()
	s.visited[requestID] = true
	return nil
}

// IsVisited satisfies storage.Storage.IsVisited.
func (s *collyStorage) IsVisited(requestID uint64) (bool, error) {
	s.RLock()
	defer s.RUnlock()
	return s.visited[requestID], nil
}

// Cookies satisfies storage.Storage.Cookies.
func (s *collyStorage) Cookies(u *url.URL) string {
	s.RLock()
	defer s.RUnlock()
	return storage.StringifyCookies(s.Storage.Cookies(u))
}

// SetCookies satisfies storage.Storage.SetCookies.
func (s *collyStorage) SetCookies(u *url.URL, cookies string) {
	s.Lock()
	defer s.Unlock()
	s.Storage.SetCookies(u, storage.UnstringifyCookies(cookies))
}
