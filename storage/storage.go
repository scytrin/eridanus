package storage

import (
	_ "image/gif"  // image decoding
	_ "image/jpeg" // image decoding
	_ "image/png"  // image decoding

	"github.com/scytrin/eridanus"
	"github.com/scytrin/eridanus/storage/classes"
	"github.com/scytrin/eridanus/storage/content"
	"github.com/scytrin/eridanus/storage/fetcher"
	"github.com/scytrin/eridanus/storage/parsers"
	"github.com/scytrin/eridanus/storage/tags"
	_ "golang.org/x/image/bmp"      // image decoding
	_ "golang.org/x/image/ccitt"    // image decoding
	_ "golang.org/x/image/riff"     // image decoding
	_ "golang.org/x/image/tiff"     // image decoding
	_ "golang.org/x/image/tiff/lzw" // image decoding
	_ "golang.org/x/image/vector"   // image decoding
	_ "golang.org/x/image/vp8"      // image decoding
	_ "golang.org/x/image/vp8l"     // image decoding
	_ "golang.org/x/image/webp"     // image decoding
)

//yaml.v2 https://play.golang.org/p/zt1Og9LIWNI
//yaml.v3 https://play.golang.org/p/H9WhcWSfJHT

const (
	cacheSize  = 1e6
	queueLimit = 1e3
	tmbX, tmbY = 150, 150
)

// Storage provides a default implementation of eridanus.Storage.
type Storage struct{ be eridanus.StorageBackend }

// NewStorage provides a new instance implementing Storage.
func NewStorage(be eridanus.StorageBackend) (*Storage, error) {
	return &Storage{be}, nil
}

// Backend provides the StorageBackend.
func (s *Storage) Backend() eridanus.StorageBackend {
	return s.be
}

// ClassesStorage provides a ClassesStorage.
func (s *Storage) ClassesStorage() eridanus.ClassesStorage {
	return classes.NewClassesStorage(s.be)
}

// ParsersStorage provides a ParsersStorage.
func (s *Storage) ParsersStorage() eridanus.ParsersStorage {
	return parsers.NewParsersStorage(s.be)
}

// TagStorage provides a TagStorage.
func (s *Storage) TagStorage() eridanus.TagStorage {
	return tags.NewTagStorage(s.be)
}

// ContentStorage provides a ContentStorage.
func (s *Storage) ContentStorage() eridanus.ContentStorage {
	return content.NewContentStorage(s.be)
}

// FetcherStorage provides a FetcherStorage.
func (s *Storage) FetcherStorage() eridanus.FetcherStorage {
	return fetcher.NewFetcherStorage(s.be)
}
