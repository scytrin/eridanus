package storage

import (
	"bytes"
	"context"
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
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nfnt/resize"
	"github.com/scytrin/eridanus"
	"github.com/scytrin/eridanus/idhash"
	"github.com/sirupsen/logrus"
	"go.chromium.org/luci/common/data/stringset"
	"gocloud.dev/blob"
	_ "gocloud.dev/blob/fileblob" // for local buckets
	_ "gocloud.dev/blob/memblob"  // for memory buckets
	"gocloud.dev/docstore"
	_ "gocloud.dev/docstore/memdocstore" // for memory docs
	_ "golang.org/x/image/bmp"           // image decoding
	_ "golang.org/x/image/ccitt"         // image decoding
	_ "golang.org/x/image/riff"          // image decoding
	_ "golang.org/x/image/tiff"          // image decoding
	_ "golang.org/x/image/tiff/lzw"      // image decoding
	_ "golang.org/x/image/vector"        // image decoding
	_ "golang.org/x/image/vp8"           // image decoding
	_ "golang.org/x/image/vp8l"          // image decoding
	_ "golang.org/x/image/webp"          // image decoding
	"golang.org/x/net/publicsuffix"
	"golang.org/x/xerrors"
	"gopkg.in/yaml.v2"
)

//yaml.v2 https://play.golang.org/p/zt1Og9LIWNI
//yaml.v3 https://play.golang.org/p/H9WhcWSfJHT

const (
	cacheSize        = 1e6
	queueLimit       = 1e3
	tmbX, tmbY       = 150, 150
	contentNamespace = "content"
	classesBlobKey   = "classes.yaml"
	parsersBlobKey   = "parsers.yaml"
)

type storageItem struct {
	IDHash string
	Tags   []string
}

// Storage provides a default implementation of eridanus.Storage.
type Storage struct {
	cookieJar http.CookieJar

	mux      *sync.RWMutex
	rootPath string

	bucket          *blob.Bucket
	contentBucket   *blob.Bucket
	thumbnailBucket *blob.Bucket
	metadataBucket  *blob.Bucket
	documents       *docstore.Collection

	parsers []*eridanus.Parser
	classes []*eridanus.URLClassifier
}

// NewStorage provides a new instance implementing Storage.
func NewStorage(ctx context.Context, rootPath string) (s *Storage, err error) {
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

	blobDir := "file:///" + filepath.ToSlash(s.rootPath)

	s.bucket, err = blob.OpenBucket(ctx, blobDir)
	if err != nil {
		return nil, fmt.Errorf("could not open bucket: %v", err)
	}

	if err := s.loadClassesFromStorage(ctx); err != nil {
		return nil, err
	}

	if err := s.loadParsersFromStorage(ctx); err != nil {
		return nil, err
	}

	s.contentBucket, err = blob.OpenBucket(ctx, blobDir+"?prefix=/content/")
	if err != nil {
		return nil, fmt.Errorf("could not open bucket: %v", err)
	}

	s.thumbnailBucket, err = blob.OpenBucket(ctx, blobDir+"?prefix=/thumbnail/")
	if err != nil {
		return nil, fmt.Errorf("could not open bucket: %v", err)
	}

	s.metadataBucket, err = blob.OpenBucket(ctx, blobDir+"?prefix=/metadata/")
	if err != nil {
		return nil, fmt.Errorf("could not open bucket: %v", err)
	}

	s.documents, err = docstore.OpenCollection(ctx, "mem://collection/idHash")
	if err != nil {
		return nil, fmt.Errorf("could not open collection: %v", err)
	}

	jarOpts := &cookiejar.Options{PublicSuffixList: publicsuffix.List}
	s.cookieJar, err = NewCookieJar(jarOpts)
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Storage) loadParsersFromStorage(ctx context.Context) error {
	r, err := s.GetData(ctx, parsersBlobKey, nil)
	if err != nil {
		if err != eridanus.ErrItemNotFound {
			return err
		}
		s.parsers = eridanus.DefaultConfig().GetParsers() // only if none existing
		return nil
	}
	return yaml.NewDecoder(r).Decode(&s.parsers)
}

func (s *Storage) loadClassesFromStorage(ctx context.Context) error {
	r, err := s.GetData(ctx, classesBlobKey, nil)
	if err != nil {
		if err != eridanus.ErrItemNotFound {
			return err
		}
		s.classes = eridanus.DefaultConfig().GetClasses() // only if none existing
		return nil
	}
	return yaml.NewDecoder(r).Decode(&s.classes)
}

// Close persists data to disk, then closes documents and buckets.
func (s *Storage) Close() error {
	s.mux.Lock()
	defer s.mux.Unlock()
	ctx := context.Background()

	if err := s.saveClassesToStorage(ctx); err != nil {
		logrus.Error(err)
	}
	if err := s.saveParsersToStorage(ctx); err != nil {
		logrus.Error(err)
	}
	if err := s.persistDocuments(ctx, s.documents.Query()); err != nil {
		logrus.Error(err)
	}
	if err := s.documents.Close(); err != nil {
		logrus.Error(err)
	}
	if err := s.bucket.Close(); err != nil {
		logrus.Error(err)
	}
	if err := s.contentBucket.Close(); err != nil {
		logrus.Error(err)
	}
	if err := s.thumbnailBucket.Close(); err != nil {
		logrus.Error(err)
	}
	if err := s.metadataBucket.Close(); err != nil {
		logrus.Error(err)
	}
	return nil
}

func (s *Storage) saveParsersToStorage(ctx context.Context) error {
	b, err := yaml.Marshal(s.parsers)
	if err != nil {
		return err
	}
	return s.PutData(ctx, parsersBlobKey, bytes.NewReader(b), nil)
}

func (s *Storage) saveClassesToStorage(ctx context.Context) error {
	b, err := yaml.Marshal(s.classes)
	if err != nil {
		return err
	}
	return s.PutData(ctx, classesBlobKey, bytes.NewReader(b), nil)
}

func (s *Storage) persistDocuments(ctx context.Context, q *docstore.Query) error {
	iter := q.Get(ctx)
	defer iter.Stop()
	for {
		var doc storageItem
		if err := iter.Next(ctx, &doc); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		out := bytes.NewBuffer(nil)
		if err := yaml.NewEncoder(out).Encode(doc); err != nil {
			return err
		}
		if err := s.metadataBucket.WriteAll(ctx, doc.IDHash, out.Bytes(), nil); err != nil {
			return err
		}
	}
	return nil
}

// Cookies implements the Cookies method of the http.CookieJar interface.
func (s *Storage) Cookies(u *url.URL) []*http.Cookie {
	return s.cookieJar.Cookies(u)
}

// SetCookies implements the SetCookies method of the http.CookieJar interface.
func (s *Storage) SetCookies(u *url.URL, cookies []*http.Cookie) {
	s.cookieJar.SetCookies(u, cookies)
}

// PutData stores arbitrary data.
func (s *Storage) PutData(ctx context.Context, key string, r io.Reader, opts *blob.WriterOptions) error {
	w, err := s.bucket.NewWriter(ctx, key, opts)
	if err != nil {
		return err
	}
	if _, err := w.ReadFrom(r); err != nil {
		return err
	}
	return w.Close()
}

// GetData fetches arbitrary data.
func (s *Storage) GetData(ctx context.Context, key string, opts *blob.ReaderOptions) (io.Reader, error) {
	has, err := s.bucket.Exists(ctx, key)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, eridanus.ErrItemNotFound
	}
	r, err := s.bucket.NewReader(ctx, key, opts)
	if err != nil {
		return nil, err
	}
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if err := r.Close(); err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// GetAllParsers returns all current parsers.
func (s *Storage) GetAllParsers(ctx context.Context) []*eridanus.Parser {
	return s.parsers
}

// AddParser adds a parser.
func (s *Storage) AddParser(ctx context.Context, p *eridanus.Parser) error {
	s.parsers = append(s.parsers, p)
	return nil
}

// GetParserByName returns the named parser.
func (s *Storage) GetParserByName(ctx context.Context, name string) (*eridanus.Parser, error) {
	for _, p := range s.parsers {
		if p.Name == name {
			return p, nil
		}
	}
	return nil, xerrors.Errorf("no parser named %s", name)
}

// FindParsers returns a list of parsers applicable to the provided URLCLassifier.
func (s *Storage) FindParsers(ctx context.Context, c *eridanus.URLClassifier) ([]*eridanus.Parser, error) {
	return nil, xerrors.New("not yet implemented")
}

// GetAllClassifiers returns all current classifiers.
func (s *Storage) GetAllClassifiers(ctx context.Context) []*eridanus.URLClassifier {
	return s.classes
}

// AddClassifier adds a classifier.
func (s *Storage) AddClassifier(ctx context.Context, c *eridanus.URLClassifier) error {
	s.classes = append(s.classes, c)
	return nil
}

// GetClassifierByName returns the named classifier.
func (s *Storage) GetClassifierByName(ctx context.Context, name string) (*eridanus.URLClassifier, error) {
	for _, c := range s.classes {
		if c.Name == name {
			return c, nil
		}
	}
	return nil, xerrors.Errorf("no classifier named %s", name)
}

// FindClassifier returns a list of parsers applicable to the provided URL.
func (s *Storage) FindClassifier(ctx context.Context, u *url.URL) (*eridanus.URLClassifier, error) {
	return nil, xerrors.New("not yet implemented")
}

// ContentKeys returns a list of all content item keys.
func (s *Storage) ContentKeys() ([]string, error) {
	ctx := context.Background()
	var keys []string
	var obj *blob.ListObject
	var err error
	for iter := s.contentBucket.List(nil); obj != nil; obj, err = iter.Next(ctx) {
		if err != nil {
			logrus.Error(err)
			continue
		}
		if obj.IsDir {
			continue
		}
		keys = append(keys, path.Base(obj.Key))
	}
	return keys, nil
}

// HasContent checks of the presence of content for the given hash.
func (s *Storage) HasContent(idHash string) bool {
	b, err := s.contentBucket.Exists(context.Background(), idHash)
	if err != nil {
		logrus.Error(err)
		return false
	}
	return b
}

// PutContent adds content, returning the hash.
func (s *Storage) PutContent(ctx context.Context, r io.Reader) (out string, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cBytes, err := ioutil.ReadAll(r)
	if err != nil {
		return "", err
	}

	idHash, err := idhash.IDHash(bytes.NewReader(cBytes))
	if err != nil {
		return "", err
	}

	if err := s.contentBucket.WriteAll(ctx, idHash, cBytes, nil); err != nil {
		return "", err
	}

	if err := s.generateThumbnail(ctx, idHash); err != nil {
		return "", err
	}

	return idHash, nil
}

func (s *Storage) generateThumbnail(ctx context.Context, idHash string) (err error) {
	defer eridanus.RecoveryHandler(func(e error) { err = e })

	r, err := s.GetContent(idHash)
	if err != nil {
		return err
	}

	img, _, err := image.Decode(r)
	if err != nil {
		return err
	}

	tImg := resize.Resize(150, 150, img, resize.NearestNeighbor)
	tw, err := s.thumbnailBucket.NewWriter(ctx, idHash, nil)
	if err != nil {
		return err
	}
	if err := png.Encode(tw, tImg); err != nil {
		return err
	}
	return tw.Close()
}

// GetContent provides a reader of the content for the given hash.
func (s *Storage) GetContent(idHash string) (io.Reader, error) {
	data, err := s.contentBucket.ReadAll(context.Background(), idHash)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

// GetThumbnail provides a reader of the thumbnail for the given hash.
func (s *Storage) GetThumbnail(idHash string) (io.Reader, error) {
	ctx := context.Background()
	for {
		data, err := s.thumbnailBucket.ReadAll(ctx, idHash)
		if err != nil {
			if false { // not found
				if err := s.generateThumbnail(ctx, idHash); err != nil {
					return nil, err
				}
				continue
			}
			return nil, err
		}
		return bytes.NewReader(data), nil
	}
}

// GetTags provides a string slice of tags for the given hash.
func (s *Storage) GetTags(idHash string) ([]string, error) {
	ctx := context.Background()

	doc := &storageItem{IDHash: idHash}
	if err := s.documents.Get(ctx, doc); err != nil {
		if !strings.Contains(err.Error(), "code=NotFound") { // isolate NotFound errors
			return nil, err
		}
		newdoc, err := s.defrostMetadata(ctx, idHash)
		if err != nil {
			return nil, err
		}
		doc = newdoc
	}

	tagSet := stringset.NewFromSlice(doc.Tags...)
	return tagSet.ToSortedSlice(), nil
}

func (s *Storage) defrostMetadata(ctx context.Context, idHash string) (*storageItem, error) {
	has, err := s.metadataBucket.Exists(ctx, idHash)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, eridanus.ErrItemNotFound
	}
	r, err := s.metadataBucket.NewReader(ctx, idHash, nil)
	if err != nil {
		return nil, err
	}
	var doc storageItem
	if err := yaml.NewDecoder(r).Decode(&doc); err != nil {
		return nil, err
	}
	if err := r.Close(); err != nil {
		return nil, err
	}
	if err := s.documents.Put(ctx, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// PutTags sets a string slice of tags for the given hash.
func (s *Storage) PutTags(idHash string, tags []string) error {
	tagSet := stringset.NewFromSlice(tags...)
	return s.documents.Put(context.Background(), &storageItem{
		IDHash: idHash,
		Tags:   tagSet.ToSortedSlice(),
	})
}

// Find searches through tags for matches.
func (s *Storage) Find() ([]string, error) {
	return nil, nil
}
