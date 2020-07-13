package storage

import (
	"bytes"
	_ "image/gif"  // image decoding
	_ "image/jpeg" // image decoding
	_ "image/png"  // image decoding
	"net/http/cookiejar"
	"os"
	"sync"

	"github.com/scytrin/eridanus"
	"github.com/scytrin/eridanus/storage/backend"
	"github.com/scytrin/eridanus/storage/classes"
	"github.com/scytrin/eridanus/storage/content"
	"github.com/scytrin/eridanus/storage/fetcher"
	"github.com/scytrin/eridanus/storage/parsers"
	"github.com/scytrin/eridanus/storage/tags"
	"github.com/sirupsen/logrus"
	_ "golang.org/x/image/bmp"      // image decoding
	_ "golang.org/x/image/ccitt"    // image decoding
	_ "golang.org/x/image/riff"     // image decoding
	_ "golang.org/x/image/tiff"     // image decoding
	_ "golang.org/x/image/tiff/lzw" // image decoding
	_ "golang.org/x/image/vector"   // image decoding
	_ "golang.org/x/image/vp8"      // image decoding
	_ "golang.org/x/image/vp8l"     // image decoding
	_ "golang.org/x/image/webp"     // image decoding
	"golang.org/x/net/publicsuffix"
	"gopkg.in/yaml.v3"
)

//yaml.v2 https://play.golang.org/p/zt1Og9LIWNI
//yaml.v3 https://play.golang.org/p/H9WhcWSfJHT

const (
	cacheSize  = 1e6
	queueLimit = 1e3
	tmbX, tmbY = 150, 150

	cookiesBlobKey = "config/cookies.json"
)

type storageItem struct {
	IDHash string
	Tags   []string
}

// Storage provides a default implementation of eridanus.Storage.
type Storage struct {
	mux     *sync.RWMutex
	cookies *Jar
	eridanus.StorageBackend
}

// NewStorage provides a new instance implementing Storage.
func NewStorage(rootPath string) (*Storage, error) {
	cookies, err := NewCookieJar(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, err
	}

	s := &Storage{
		mux:            new(sync.RWMutex),
		cookies:        cookies,
		StorageBackend: backend.NewDiskvBackend(rootPath),
	}

	// if err := s.classStorage.load(); err != nil {
	// 	return nil, err
	// }

	// if err := s.parserStorage.load(); err != nil {
	// 	return nil, err
	// }

	if err := func() error { //cookie persistence
		rc, err := s.Get(cookiesBlobKey)
		if err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			return nil
		}
		defer rc.Close()
		if err := yaml.NewDecoder(rc).Decode(&s.cookies.entries); err != nil {
			return err
		}
		return nil
	}(); err != nil {
		defer s.Close()
		return nil, err
	}

	return s, nil
}

// Close persists data to disk, then closes documents and buckets.
func (s *Storage) Close() error {
	s.mux.Lock()
	defer s.mux.Unlock()

	// if err := s.parserStorage.save(); err != nil {
	// 	logrus.Error(err)
	// }
	// if err := s.classStorage.save(); err != nil {
	// 	logrus.Error(err)
	// }

	if err := func() error { // cookie persistence
		b, err := yaml.Marshal(s.cookies.entries)
		if err != nil {
			return err
		}
		return s.Set(cookiesBlobKey, bytes.NewReader(b))
	}(); err != nil {
		logrus.Error(err)
	}

	if s.StorageBackend != nil {
		if err := s.StorageBackend.Close(); err != nil {
			logrus.Error(err)
		}
	}

	return nil
}

// ClassesStorage provides a ClassesStorage.
func (s *Storage) ClassesStorage() eridanus.ClassesStorage {
	return classes.NewClassesStorage(s.StorageBackend)
}

// ParsersStorage provides a ParsersStorage.
func (s *Storage) ParsersStorage() eridanus.ParsersStorage {
	return parsers.NewParsersStorage(s.StorageBackend)
}

// TagStorage provides a TagStorage.
func (s *Storage) TagStorage() eridanus.TagStorage {
	return tags.NewTagStorage(s.StorageBackend)
}

// ContentStorage provides a ContentStorage.
func (s *Storage) ContentStorage() eridanus.ContentStorage {
	return content.NewContentStorage(s.StorageBackend)
}

// FetcherStorage provides a FetcherStorage.
func (s *Storage) FetcherStorage() eridanus.FetcherStorage {
	return fetcher.NewFetcherStorage(s.StorageBackend, s.cookies)
}
