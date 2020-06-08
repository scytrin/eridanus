package eridanus

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/ioutil"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/corona10/goimagehash"
	"github.com/gocolly/colly/v2/storage"
	cookiejar "github.com/juju/persistent-cookiejar"
	"github.com/nfnt/resize"
	"github.com/sirupsen/logrus"
	"go.chromium.org/luci/common/data/stringset"
	"gocloud.dev/blob"
	_ "gocloud.dev/blob/fileblob" // for local buckets
	_ "gocloud.dev/blob/memblob"  // for memory buckets
	"gocloud.dev/docstore"
	_ "gocloud.dev/docstore/memdocstore" // for memory docs
	"golang.org/x/net/publicsuffix"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v2"
)

const (
	cacheSize  = 1e6
	queueLimit = 1e3
	tmbX, tmbY = 150, 150
)

var (
	// ErrBlobNotFound is an identifiable error for "NotFound"
	ErrBlobNotFound = errors.New("not found")
)

// Storage manages content.
type Storage interface {
	AsCollyStorage() storage.Storage

	Close() error

	PutData(ctx context.Context, path string, r io.Reader, opts *blob.WriterOptions) error
	GetData(ctx context.Context, path string, opts *blob.ReaderOptions) (io.Reader, error)

	PutTags(idHash string, tags []string) error
	GetTags(idHash string) ([]string, error)

	PutContent(ctx context.Context, r io.Reader) (idHash string, err error)
	GetContent(idHash string) (io.Reader, error)

	GetThumbnail(idHash string) (io.Reader, error)

	// Find() ([]string, error)
}

type storageItem struct {
	IDHash string
	Tags   []string
}

type storageImpl struct {
	mux      *sync.RWMutex
	onClose  []io.Closer
	rootPath string

	bucket, contentBucket, thumbnailBucket, metadataBucket *blob.Bucket

	documents *docstore.Collection

	collyVisited map[uint64]bool
	collyCookies *cookiejar.Jar
}

// NewStorage provides a new instance implementing Storage.
func NewStorage(rootPath string) (Storage, error) {
	ctx := context.Background()

	blobDir := "file:///" + filepath.ToSlash(rootPath)
	bucket, err := blob.OpenBucket(ctx, blobDir)
	if err != nil {
		return nil, fmt.Errorf("could not open bucket: %v", err)
	}
	contentBucket, err := blob.OpenBucket(ctx, blobDir+"?prefix=/content/")
	if err != nil {
		return nil, fmt.Errorf("could not open bucket: %v", err)
	}
	thumbnailBucket, err := blob.OpenBucket(ctx, blobDir+"?prefix=/thumbnail/")
	if err != nil {
		return nil, fmt.Errorf("could not open bucket: %v", err)
	}
	metadataBucket, err := blob.OpenBucket(ctx, blobDir+"?prefix=/metadata/")
	if err != nil {
		return nil, fmt.Errorf("could not open bucket: %v", err)
	}

	documents, err := docstore.OpenCollection(ctx, "mem://collection/idHash")
	if err != nil {
		return nil, fmt.Errorf("could not open collection: %v", err)
	}

	jar, err := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
		Filename:         filepath.Join(rootPath, "cookies"),
	})
	if err != nil {
		return nil, err
	}

	s := &storageImpl{
		mux:             new(sync.RWMutex),
		rootPath:        rootPath,
		bucket:          bucket,
		contentBucket:   contentBucket,
		thumbnailBucket: thumbnailBucket,
		metadataBucket:  metadataBucket,
		documents:       documents,
		collyVisited:    make(map[uint64]bool),
		collyCookies:    jar,
	}
	s.onClose = []io.Closer{
		documents,
		bucket,
		contentBucket,
		thumbnailBucket,
		metadataBucket,
	}

	return s, nil
}

func (s *storageImpl) Close() error {
	s.mux.Lock()
	defer s.mux.Unlock()

	ctx := context.Background()
	iter := s.documents.Query().Get(ctx)
	defer iter.Stop()
	for {
		var doc storageItem
		if err := iter.Next(ctx, &doc); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		out, err := yaml.Marshal(doc)
		if err != nil {
			return err
		}
		if err := s.metadataBucket.WriteAll(ctx, doc.IDHash, out, nil); err != nil {
			return err
		}
	}

	if err := s.collyCookies.Save(); err != nil {
		return err
	}

	var errg errgroup.Group
	for _, c := range s.onClose {
		c := c
		errg.Go(func() error {
			err := c.Close()
			if err != nil {
				logrus.Errorf("failed to close: %v\n%v", c, err)
			}
			return err
		})
	}

	return errg.Wait()
}

// PutData stores arbitrary data.
func (s *storageImpl) PutData(ctx context.Context, key string, r io.Reader, opts *blob.WriterOptions) error {
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
func (s *storageImpl) GetData(ctx context.Context, key string, opts *blob.ReaderOptions) (io.Reader, error) {
	has, err := s.bucket.Exists(ctx, key)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrBlobNotFound
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

func (s *storageImpl) ContentKeys() []string {
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
	return keys
}

func (s *storageImpl) HasContent(idHash string) bool {
	b, err := s.contentBucket.Exists(context.Background(), idHash)
	if err != nil {
		logrus.Error(err)
		return false
	}
	return b
}

func (s *storageImpl) PutContent(ctx context.Context, r io.Reader) (out string, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	cBytes, err := ioutil.ReadAll(r)
	if err != nil {
		return "", err
	}

	idHash, err := IDHash(bytes.NewReader(cBytes))
	if err != nil {
		return "", err
	}

	if err := s.contentBucket.WriteAll(ctx, idHash, cBytes, nil); err != nil {
		return "", err
	}

	if err := s.generateThumbnail(ctx, idHash); err != nil {
		return "", err
	}

	if err := s.updateContentPHash(idHash); err != nil {
		return "", err
	}

	return idHash, nil
}

func (s *storageImpl) generateThumbnail(ctx context.Context, idHash string) (err error) {
	defer func() {
		if rerr, ok := recover().(error); rerr != nil && ok {
			err = rerr
		}
	}()

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

func (s *storageImpl) updateContentPHash(idHash string) (err error) {
	defer func() {
		if rerr, ok := recover().(error); rerr != nil && ok {
			err = rerr
		}
	}()

	tags, err := s.GetTags(idHash)
	if err != nil {
		return err
	}

	r, err := s.GetContent(idHash)
	if err != nil {
		return err
	}

	img, _, err := image.Decode(r)
	if err != nil {
		return err
	}

	pHash, err := goimagehash.PerceptionHash(img)
	if err != nil {
		return err
	}

	if pHash.GetHash() == 0 {
		return errors.New("no phash generated")
	}

	tags = append(tags, fmt.Sprintf("phash:%s", pHash.ToString()))
	return s.PutTags(idHash, tags)
}

func (s *storageImpl) GetContent(idHash string) (io.Reader, error) {
	data, err := s.contentBucket.ReadAll(context.Background(), idHash)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (s *storageImpl) GetThumbnail(idHash string) (io.Reader, error) {
	data, err := s.thumbnailBucket.ReadAll(context.Background(), idHash)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (s *storageImpl) GetTags(idHash string) ([]string, error) {
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

func (s *storageImpl) defrostMetadata(ctx context.Context, idHash string) (*storageItem, error) {
	has, err := s.metadataBucket.Exists(ctx, idHash)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrBlobNotFound
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

func (s *storageImpl) PutTags(idHash string, tags []string) error {
	tagSet := stringset.NewFromSlice(tags...)
	return s.documents.Put(context.Background(), &storageItem{
		IDHash: idHash,
		Tags:   tagSet.ToSortedSlice(),
	})
}

type collyStorage struct {
	sync.RWMutex
	*storageImpl
}

func (s *storageImpl) AsCollyStorage() storage.Storage {
	return &collyStorage{storageImpl: s}
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
	return storage.StringifyCookies(s.collyCookies.Cookies(u))
}

// SetCookies satisfies storage.Storage.SetCookies.
func (s *collyStorage) SetCookies(u *url.URL, cookies string) {
	s.Lock()
	defer s.Unlock()
	s.collyCookies.SetCookies(u, storage.UnstringifyCookies(cookies))
}
