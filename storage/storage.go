package storage

import (
	"bufio"
	"bytes"
	"crypto/md5"
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

	"github.com/golang/protobuf/proto"
	"github.com/nfnt/resize"
	"github.com/pkg/errors"
	"github.com/scytrin/eridanus"
	"github.com/scytrin/eridanus/idhash"
	"github.com/scytrin/eridanus/storage/backend"
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
	cacheSize          = 1e6
	queueLimit         = 1e3
	tmbX, tmbY         = 150, 150
	contentNamespace   = "content"
	thumbnailNamespace = "thumbnail"
	metadataNamespace  = "metadata"
	webcacheNamespace  = "web_cache"
	webresultNamespace = "web_result"
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
	eridanus.StorageBackend
	tagStorage
	fetcherStorage
	contentStorage
	classStorage
	parserStorage

	mux *sync.RWMutex
}

// NewStorage provides a new instance implementing Storage.
func NewStorage(rootPath string) (*Storage, error) {
	cookies, err := NewCookieJar(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, err
	}

	be := backend.NewDiskvBackend(rootPath)

	s := &Storage{
		StorageBackend: be,
		classStorage:   classStorage{be, nil},
		parserStorage:  parserStorage{be, nil},
		fetcherStorage: fetcherStorage{be, cookies},
		contentStorage: contentStorage{be},
		tagStorage:     tagStorage{be},
		mux:            new(sync.RWMutex),
	}

	if err := s.classStorage.load(); err != nil {
		return nil, err
	}

	if err := s.parserStorage.load(); err != nil {
		return nil, err
	}

	if err := s.fetcherStorage.load(); err != nil {
		return nil, err
	}

	return s, nil
}

// Close persists data to disk, then closes documents and buckets.
func (s *Storage) Close() error {
	s.mux.Lock()
	defer s.mux.Unlock()

	if err := s.parserStorage.save(); err != nil {
		logrus.Error(err)
	}
	if err := s.classStorage.save(); err != nil {
		logrus.Error(err)
	}
	if err := s.fetcherStorage.save(); err != nil {
		logrus.Error(err)
	}

	if s.StorageBackend != nil {
		if err := s.StorageBackend.Close(); err != nil {
			logrus.Error(err)
		}
	}

	return nil
}

type parserStorage struct {
	eridanus.StorageBackend
	parsers []*eridanus.Parser
}

func (s *parserStorage) I() eridanus.ParsersStorage { return s }

func (s *parserStorage) load() error {
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

func (s *parserStorage) save() error {
	b, err := yaml.Marshal(s.parsers)
	if err != nil {
		return err
	}
	return s.SetData(parsersBlobKey, bytes.NewReader(b))
}

// GetAllParsers returns all current parsers.
func (s *parserStorage) GetAllParsers() []*eridanus.Parser {
	return s.parsers
}

// AddParser adds a parser.
func (s *parserStorage) AddParser(p *eridanus.Parser) error {
	s.parsers = append(s.parsers, p)
	return nil
}

// GetParserByName returns the named parser.
func (s *parserStorage) GetParserByName(name string) (*eridanus.Parser, error) {
	for _, p := range s.parsers {
		if p.Name == name {
			return p, nil
		}
	}
	return nil, errors.Errorf("no parser named %s", name)
}

// ParsersFor returns a list of parsers applicable to the provided URLClass.
func (s *parserStorage) ParsersFor(c *eridanus.URLClass) ([]*eridanus.Parser, error) {
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

type classStorage struct {
	eridanus.StorageBackend
	classes []*eridanus.URLClass
}

func (s *classStorage) I() eridanus.ClassesStorage { return s }

func (s *classStorage) load() error {
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

func (s *classStorage) save() error {
	b, err := yaml.Marshal(s.classes)
	if err != nil {
		return err
	}
	return s.SetData(classesBlobKey, bytes.NewReader(b))
}

// GetAllClassifiers returns all current classifiers.
func (s *classStorage) GetAllClassifiers() []*eridanus.URLClass {
	return s.classes
}

// AddClassifier adds a classifier.
func (s *classStorage) AddClassifier(c *eridanus.URLClass) error {
	s.classes = append(s.classes, c)
	return nil
}

// GetClassifierByName returns the named classifier.
func (s *classStorage) GetClassifierByName(name string) (*eridanus.URLClass, error) {
	for _, c := range s.classes {
		if c.Name == name {
			return c, nil
		}
	}
	return nil, errors.Errorf("no classifier named %s", name)
}

type contentStorage struct{ eridanus.StorageBackend }

func (s *contentStorage) I() eridanus.ContentStorage { return s }

// ContentKeys returns a list of all content item keys.
func (s *contentStorage) ContentKeys() (eridanus.IDHashes, error) {
	var idHashes eridanus.IDHashes
	keys, err := s.Keys(contentNamespace)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		idHashes = append(idHashes, eridanus.IDHash(k))
	}
	return idHashes, nil
}

// HasContent checks of the presence of content for the given hash.
func (s *contentStorage) HasContent(idHash eridanus.IDHash) bool {
	cPath := fmt.Sprintf("%s/%s", contentNamespace, idHash)
	return s.HasData(cPath)
}

// SetContent adds content, returning the hash.
func (s *contentStorage) SetContent(r io.Reader) (out eridanus.IDHash, err error) {
	cBytes, err := ioutil.ReadAll(r)
	if err != nil {
		return "", err
	}

	idHash, err := idhash.GenerateIDHash(bytes.NewReader(cBytes))
	if err != nil {
		return "", err
	}

	cPath := fmt.Sprintf("%s/%s", contentNamespace, idHash)
	if err := s.SetData(cPath, bytes.NewReader(cBytes)); err != nil {
		return "", err
	}

	return idHash, nil
}

func (s *contentStorage) generateThumbnail(idHash eridanus.IDHash) (err error) {
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
	return s.SetData(tPath, tBuf)
}

// GetContent provides a reader of the content for the given hash.
func (s *contentStorage) GetContent(idHash eridanus.IDHash) (io.ReadCloser, error) {
	cPath := fmt.Sprintf("%s/%s", contentNamespace, idHash)
	return s.GetData(cPath)
}

// GetThumbnail provides a reader of the thumbnail for the given hash.
func (s *contentStorage) GetThumbnail(idHash eridanus.IDHash) (io.ReadCloser, error) {
	tPath := fmt.Sprintf("%s/%s", thumbnailNamespace, idHash)
	if !s.HasData(tPath) {
		if err := s.generateThumbnail(idHash); err != nil {
			return nil, err
		}
	}
	return s.GetData(tPath)
}

type tagStorage struct{ eridanus.StorageBackend }

func (s *tagStorage) I() eridanus.TagStorage { return s }

// TagKeys returns a list of all tag item keys.
func (s *tagStorage) TagKeys() (eridanus.IDHashes, error) {
	var idHashes eridanus.IDHashes
	keys, err := s.Keys(metadataNamespace)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		idHashes = append(idHashes, eridanus.IDHash(k))
	}
	return idHashes, nil
}

// GetTags provides a string slice of tags for the given hash.
func (s *tagStorage) GetTags(idHash eridanus.IDHash) (eridanus.Tags, error) {
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
	tagStrs := strings.Split(string(b), ",")
	var tags eridanus.Tags
	for _, t := range tagStrs {
		tags = append(tags, eridanus.Tag(t))
	}
	return tags.OmitDuplicates(), nil
}

// HasTags indicates if tags exist for the given hash.
func (s *tagStorage) HasTags(idHash eridanus.IDHash) bool {
	mPath := fmt.Sprintf("%s/%s", metadataNamespace, idHash)
	return s.StorageBackend.HasData(mPath)
}

// SetTags sets a string slice of tags for the given hash.
func (s *tagStorage) SetTags(idHash eridanus.IDHash, newTags eridanus.Tags) error {
	mPath := fmt.Sprintf("%s/%s", metadataNamespace, idHash)
	tagStr := newTags.OmitDuplicates().String()
	return s.SetData(mPath, strings.NewReader(tagStr))
}

// Find searches through tags for matches.
func (s *tagStorage) FindByTags() (eridanus.IDHashes, error) {
	var idHashes eridanus.IDHashes
	keys, err := s.Keys(metadataNamespace)
	if err != nil {
		return nil, err
	}
	for _, mPath := range keys {
		idHash := eridanus.IDHash(filepath.Base(mPath))
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

type fetcherStorage struct {
	eridanus.StorageBackend
	cookies *Jar
}

func (s *fetcherStorage) I() eridanus.FetcherStorage { return s }

func (s *fetcherStorage) GetResults(u *url.URL) (*eridanus.ParseResults, error) {
	hsh := fmt.Sprintf("%x", md5.Sum([]byte(u.String())))
	rPath := fmt.Sprintf("%s/%s", webresultNamespace, hsh)
	var r eridanus.ParseResults
	rc, err := s.GetData(rPath)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	d, err := ioutil.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	if err := proto.UnmarshalText(string(d), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *fetcherStorage) SetResults(u *url.URL, r *eridanus.ParseResults) error {
	hsh := fmt.Sprintf("%x", md5.Sum([]byte(u.String())))
	rPath := fmt.Sprintf("%s/%s", webresultNamespace, hsh)
	return s.SetData(rPath, strings.NewReader(proto.CompactTextString(r)))
}

func (s *fetcherStorage) GetCached(u *url.URL) (*http.Response, error) {
	hsh := fmt.Sprintf("%x", md5.Sum([]byte(u.String())))
	cPath := fmt.Sprintf("%s/%s", webcacheNamespace, hsh)
	rc, err := s.GetData(cPath)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var reqSize int64
	if _, err := fmt.Fscanln(rc, &reqSize); err != nil {
		return nil, err
	}

	reqBuf := io.LimitReader(rc, int64(reqSize))
	req, err := http.ReadRequest(bufio.NewReader(reqBuf))
	if err != nil {
		return nil, err
	}

	var resSize int64
	if _, err := fmt.Fscanln(rc, &resSize); err != nil {
		return nil, err
	}

	resBuf := io.LimitReader(rc, int64(resSize))
	res, err := http.ReadResponse(bufio.NewReader(resBuf), req)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (s *fetcherStorage) SetCached(u *url.URL, res *http.Response) error {
	hsh := fmt.Sprintf("%x", md5.Sum([]byte(u.String())))
	cPath := fmt.Sprintf("%s/%s", webcacheNamespace, hsh)

	resBuf := bytes.NewBuffer(nil)
	res.Write(resBuf)

	reqBuf := bytes.NewBuffer(nil)
	if res.Request != nil {
		res.Request.Write(reqBuf)
	}

	buf := bytes.NewBuffer(nil)
	fmt.Fprintf(buf, "%d\n%s", reqBuf.Len(), reqBuf.Bytes())
	fmt.Fprintf(buf, "%d\n%s", resBuf.Len(), resBuf.Bytes())

	return s.SetData(cPath, buf)
}

func (s *fetcherStorage) load() error {
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

func (s *fetcherStorage) save() error {
	b, err := yaml.Marshal(s.cookies.entries)
	if err != nil {
		return err
	}
	return s.SetData(cookiesBlobKey, bytes.NewReader(b))
}

// Cookies implements the Cookies method of the http.CookieJar interface.
func (s *fetcherStorage) Cookies(u *url.URL) []*http.Cookie {
	cookies := s.cookies.Cookies(u)
	logrus.WithField("cookie", "get").WithField("url", u).Debug(cookies)
	return cookies
}

// SetCookies implements the SetCookies method of the http.CookieJar interface.
func (s *fetcherStorage) SetCookies(u *url.URL, cookies []*http.Cookie) {
	logrus.WithField("cookie", "set").WithField("url", u).Debug(cookies)
	s.cookies.SetCookies(u, cookies)
}
