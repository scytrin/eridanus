package storage

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // image decoding
	_ "image/jpeg" // image decoding
	"image/png"
	_ "image/png" // image decoding
	"io"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nfnt/resize"
	"github.com/peterbourgon/diskv/v3"
	"github.com/pkg/errors"
	"github.com/scytrin/eridanus"
	"github.com/scytrin/eridanus/idhash"
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
	"gopkg.in/yaml.v2"
)

//yaml.v2 https://play.golang.org/p/zt1Og9LIWNI
//yaml.v3 https://play.golang.org/p/H9WhcWSfJHT

const (
	cacheSize          = 1e6
	queueLimit         = 1e3
	tmbX, tmbY         = 150, 150
	contentNamespace   = "content"
	thumbnailNamespace = "thumbnail"
	metadataNamespace  = "metadata"
	cookiesBlobKey     = "config/cookies.json"
	classesBlobKey     = "config/classes.yaml"
	parsersBlobKey     = "config/parsers.yaml"
)

type storageItem struct {
	IDHash string
	Tags   []string
}

// Storage provides a default implementation of eridanus.Storage.
type Storage struct {
	mux      *sync.RWMutex
	rootPath string
	backend  eridanus.StorageBackend

	kvStore *diskv.Diskv

	parsers []*eridanus.Parser
	classes []*eridanus.URLClass
	cookies *Jar // http.CookieJar
}

// NewStorage provides a new instance implementing Storage.
func NewStorage(rootPath string) (s *Storage, err error) {
	s = &Storage{
		mux:      new(sync.RWMutex),
		rootPath: rootPath,
	}

	if _, err := os.Stat(s.rootPath); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := os.MkdirAll(s.rootPath, 0755); err != nil {
			return nil, err
		}
	}

	jarOpts := &cookiejar.Options{PublicSuffixList: publicsuffix.List}
	s.cookies, err = NewCookieJar(jarOpts)
	if err != nil {
		return nil, err
	}

	s.kvStore = diskv.New(diskv.Options{
		BasePath:     filepath.Join(s.rootPath, "kv"),
		CacheSizeMax: 8e+7,
		PathPerm:     0755,
		FilePerm:     0644,
		AdvancedTransform: func(s string) *diskv.PathKey {
			path := strings.Split(s, string(filepath.Separator))
			last := len(path) - 1
			return &diskv.PathKey{Path: path[:last], FileName: path[last]}
		},
		InverseTransform: func(pk *diskv.PathKey) string {
			return filepath.Join(append(pk.Path, pk.FileName)...)
		},
	})

	if err := s.loadCookiesFromStorage(); err != nil {
		return nil, err
	}

	if err := s.loadClassesFromStorage(); err != nil {
		return nil, err
	}

	if err := s.loadParsersFromStorage(); err != nil {
		return nil, err
	}

	return s, nil
}

// GetRootPath returns the filepath location of on disk storage.
func (s *Storage) GetRootPath() string {
	return s.rootPath
}

// Close persists data to disk, then closes documents and buckets.
func (s *Storage) Close() error {
	s.mux.Lock()
	defer s.mux.Unlock()

	if err := s.saveCookiesToStorage(); err != nil {
		logrus.Error(err)
	}
	if err := s.saveClassesToStorage(); err != nil {
		logrus.Error(err)
	}
	if err := s.saveParsersToStorage(); err != nil {
		logrus.Error(err)
	}

	return nil
}

func (s *Storage) loadCookiesFromStorage() error {
	rc, err := s.GetData(cookiesBlobKey)
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
}

func (s *Storage) saveCookiesToStorage() error {
	b, err := yaml.Marshal(s.cookies.entries)
	if err != nil {
		return err
	}
	return s.PutData(cookiesBlobKey, bytes.NewReader(b))
}

func (s *Storage) loadParsersFromStorage() error {
	rc, err := s.GetData(parsersBlobKey)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	defer rc.Close()
	if err := yaml.NewDecoder(rc).Decode(&s.parsers); err != nil {
		return err
	}
	if err != nil || len(s.parsers) == 0 {
		s.parsers = eridanus.DefaultParsers() // only if none existing
	}
	return nil
}

func (s *Storage) saveParsersToStorage() error {
	b, err := yaml.Marshal(s.parsers)
	if err != nil {
		return err
	}
	return s.PutData(parsersBlobKey, bytes.NewReader(b))
}

func (s *Storage) loadClassesFromStorage() error {
	rc, err := s.GetData(classesBlobKey)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	defer rc.Close()
	if err := yaml.NewDecoder(rc).Decode(&s.classes); err != nil {
		return err
	}
	if err != nil || len(s.classes) == 0 {
		s.classes = eridanus.DefaultClasses() // only if none existing
	}
	return nil
}

func (s *Storage) saveClassesToStorage() error {
	b, err := yaml.Marshal(s.classes)
	if err != nil {
		return err
	}
	return s.PutData(classesBlobKey, bytes.NewReader(b))
}

// Cookies implements the Cookies method of the http.CookieJar interface.
func (s *Storage) Cookies(u *url.URL) []*http.Cookie {
	cookies := s.cookies.Cookies(u)
	logrus.WithField("cookie", "get").WithField("url", u).Debug(cookies)
	return cookies
}

// SetCookies implements the SetCookies method of the http.CookieJar interface.
func (s *Storage) SetCookies(u *url.URL, cookies []*http.Cookie) {
	logrus.WithField("cookie", "set").WithField("url", u).Debug(cookies)
	s.cookies.SetCookies(u, cookies)
}

// PutData stores arbitrary data.
func (s *Storage) PutData(key string, r io.Reader) error {
	return s.kvStore.WriteStream(filepath.FromSlash(key), r, false)
}

// HasData checks for the presence of arbitrary data.
func (s *Storage) HasData(key string) bool {
	return s.kvStore.Has(filepath.FromSlash(key))
}

// GetData fetches arbitrary data.
func (s *Storage) GetData(key string) (io.ReadCloser, error) {
	return s.kvStore.ReadStream(filepath.FromSlash(key), false)
}

// DeleteData removes arbitrary data.
func (s *Storage) DeleteData(key string) error {
	return s.kvStore.Erase(filepath.FromSlash(key))
}

// Keys returns a list of all keys under the provided prefix.
func (s *Storage) Keys(prefix string) ([]string, error) {
	var keys []string
	for key := range s.kvStore.KeysPrefix(prefix, nil) {
		keys = append(keys, key)
	}
	return keys, nil
}

// GetAllParsers returns all current parsers.
func (s *Storage) GetAllParsers() []*eridanus.Parser {
	return s.parsers
}

// AddParser adds a parser.
func (s *Storage) AddParser(p *eridanus.Parser) error {
	s.parsers = append(s.parsers, p)
	return nil
}

// GetParserByName returns the named parser.
func (s *Storage) GetParserByName(name string) (*eridanus.Parser, error) {
	for _, p := range s.parsers {
		if p.Name == name {
			return p, nil
		}
	}
	return nil, errors.Errorf("no parser named %s", name)
}

// ParsersFor returns a list of parsers applicable to the provided URLClass.
func (s *Storage) ParsersFor(c *eridanus.URLClass) ([]*eridanus.Parser, error) {
	var keep []*eridanus.Parser

	pts := eridanus.ClassifierParserTypes[c.GetClass()]
	for _, p := range s.GetAllParsers() {
		var has bool
		for _, pt := range pts {
			has = has || p.GetType() == pt
		}
		if !has {
			continue
		}
		for _, ru := range p.GetUrls() {
			u, err := url.Parse(ru)
			if err != nil {
				return nil, err
			}
			if _, err := eridanus.ApplyClassifier(c, u); err == nil {
				keep = append(keep, p)
				break
			}
		}
	}
	return keep, nil
}

// GetAllClassifiers returns all current classifiers.
func (s *Storage) GetAllClassifiers() []*eridanus.URLClass {
	return s.classes
}

// AddClassifier adds a classifier.
func (s *Storage) AddClassifier(c *eridanus.URLClass) error {
	s.classes = append(s.classes, c)
	return nil
}

// GetClassifierByName returns the named classifier.
func (s *Storage) GetClassifierByName(name string) (*eridanus.URLClass, error) {
	for _, c := range s.classes {
		if c.Name == name {
			return c, nil
		}
	}
	return nil, errors.Errorf("no classifier named %s", name)
}

// ContentKeys returns a list of all content item keys.
func (s *Storage) ContentKeys() ([]string, error) {
	return s.Keys(contentNamespace)
}

// HasContent checks of the presence of content for the given hash.
func (s *Storage) HasContent(idHash string) bool {
	cPath := fmt.Sprintf("%s/%s", contentNamespace, idHash)
	return s.HasData(cPath)
}

// PutContent adds content, returning the hash.
func (s *Storage) PutContent(r io.Reader) (out string, err error) {
	cBytes, err := ioutil.ReadAll(r)
	if err != nil {
		return "", err
	}

	idHash, err := idhash.IDHash(bytes.NewReader(cBytes))
	if err != nil {
		return "", err
	}

	cPath := fmt.Sprintf("%s/%s", contentNamespace, idHash)
	if err := s.PutData(cPath, bytes.NewReader(cBytes)); err != nil {
		return "", err
	}

	return idHash, nil
}

func (s *Storage) generateThumbnail(idHash string) (err error) {
	defer eridanus.RecoveryHandler(func(e error) { err = e })

	r, err := s.GetContent(idHash)
	if err != nil {
		return err
	}

	img, _, err := image.Decode(r)
	if err != nil {
		return err
	}

	tBuf := bytes.NewBuffer(nil)
	tImg := resize.Thumbnail(150, 150, img, resize.NearestNeighbor)
	if err := png.Encode(tBuf, tImg); err != nil {
		return err
	}

	tPath := fmt.Sprintf("%s/%s", thumbnailNamespace, idHash)
	return s.PutData(tPath, tBuf)
}

// GetContent provides a reader of the content for the given hash.
func (s *Storage) GetContent(idHash string) (io.ReadCloser, error) {
	cPath := fmt.Sprintf("%s/%s", contentNamespace, idHash)
	return s.GetData(cPath)
}

// GetThumbnail provides a reader of the thumbnail for the given hash.
func (s *Storage) GetThumbnail(idHash string) (io.ReadCloser, error) {
	tPath := fmt.Sprintf("%s/%s", thumbnailNamespace, idHash)
	if !s.HasData(tPath) {
		if err := s.generateThumbnail(idHash); err != nil {
			return nil, err
		}
	}
	return s.GetData(tPath)
}

// GetTags provides a string slice of tags for the given hash.
func (s *Storage) GetTags(idHash string) ([]string, error) {
	mPath := fmt.Sprintf("%s/%s", metadataNamespace, idHash)
	r, err := s.GetData(mPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	tags := strings.Split(string(b), ",")
	return eridanus.RemoveDuplicateStrings(tags), nil
}

// PutTags sets a string slice of tags for the given hash.
func (s *Storage) PutTags(idHash string, newTags []string) error {
	mPath := fmt.Sprintf("%s/%s", metadataNamespace, idHash)
	tags := eridanus.RemoveDuplicateStrings(newTags)
	return s.PutData(mPath, strings.NewReader(strings.Join(tags, ",")))
}

// Find searches through tags for matches.
func (s *Storage) Find() ([]string, error) {
	var idHashes []string
	keys, err := s.Keys(metadataNamespace)
	if err != nil {
		return nil, err
	}
	for _, mPath := range keys {
		idHash := filepath.Base(mPath)
		tags, err := s.GetTags(idHash)
		if err != nil {
			return nil, err
		}
		if len(tags) > 0 {
			idHashes = append(idHashes, idHash)
		}
	}
	return idHashes, nil
}
