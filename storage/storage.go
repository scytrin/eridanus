package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/corona10/goimagehash"
	"github.com/nfnt/resize"
	"github.com/scytrin/eridanus/idhash"
	"github.com/sirupsen/logrus"
	"go.chromium.org/luci/common/data/stringset"
	"gocloud.dev/blob"
	_ "gocloud.dev/blob/fileblob" // for local buckets
	_ "gocloud.dev/blob/memblob"  // for memory buckets
	"gocloud.dev/docstore"
	_ "gocloud.dev/docstore/memdocstore" // for memory docs
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

type storageItem struct {
	IDHash string
	Tags   []string
}

// Storage provides a default implementation of eridanus.Storage.
type Storage struct {
	mux     *sync.RWMutex
	onClose []io.Closer

	rootPath     string
	cookieJar    *cookieJar // http.CookieJar
	collyVisited map[uint64]bool

	bucket          *blob.Bucket
	contentBucket   *blob.Bucket
	thumbnailBucket *blob.Bucket
	metadataBucket  *blob.Bucket
	documents       *docstore.Collection
}

// NewStorage provides a new instance implementing Storage.
func NewStorage(ctx context.Context, rootPath string) (s *Storage, err error) {
	s = &Storage{
		mux:          new(sync.RWMutex),
		rootPath:     rootPath,
		cookieJar:    new(cookieJar),
		collyVisited: make(map[uint64]bool),
	}

	if _, err := os.Stat(rootPath); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := os.MkdirAll(rootPath, 0755); err != nil {
			return nil, err
		}
	}

	blobDir := "file:///" + filepath.ToSlash(rootPath)

	s.bucket, err = blob.OpenBucket(ctx, blobDir)
	if err != nil {
		return nil, fmt.Errorf("could not open bucket: %v", err)
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

	return s, nil
}

// CookieJar provides a storage consistent http.CookieJar.
func (s *Storage) CookieJar() http.CookieJar {
	return s.cookieJar
}

// Close persists data to disk, then closes documents and buckets.
func (s *Storage) Close() error {
	s.mux.Lock()
	defer s.mux.Unlock()
	ctx := context.Background()
	var errg errgroup.Group

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
		logrus.Info(doc)
		out, err := yaml.Marshal(doc)
		if err != nil {
			return err
		}
		if err := s.metadataBucket.WriteAll(ctx, doc.IDHash, out, nil); err != nil {
			return err
		}
	}

	errg.Go(s.documents.Close)

	if err := errg.Wait(); err != nil {
		return fmt.Errorf("0: %v", err)
	}

	errg.Go(s.thumbnailBucket.Close)
	errg.Go(s.contentBucket.Close)
	errg.Go(s.metadataBucket.Close)
	errg.Go(s.bucket.Close)
	for _, c := range s.onClose {
		if c != nil {
			errg.Go(c.Close)
		}
	}

	if err := errg.Wait(); err != nil {
		return fmt.Errorf("1: %v", err)
	}

	return nil
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

// ContentKeys returns a list of all content item keys.
func (s *Storage) ContentKeys() []string {
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

	if err := s.updateContentPHash(idHash); err != nil {
		return "", err
	}

	return idHash, nil
}

func recoveryHandler(f func(error)) {
	r := recover()
	if r == nil {
		return
	}
	logrus.Debug(r)
	switch rerr := r.(type) {
	case error:
		f(rerr)
	case string:
		f(errors.New(rerr))
	default:
		f(fmt.Errorf("panicked: %v", rerr))
	}
}

func (s *Storage) generateThumbnail(ctx context.Context, idHash string) (err error) {
	defer recoveryHandler(func(e error) { err = e })

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

func (s *Storage) updateContentPHash(idHash string) (err error) {
	defer recoveryHandler(func(e error) { err = e })

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

// PutTags sets a string slice of tags for the given hash.
func (s *Storage) PutTags(idHash string, tags []string) error {
	tagSet := stringset.NewFromSlice(tags...)
	return s.documents.Put(context.Background(), &storageItem{
		IDHash: idHash,
		Tags:   tagSet.ToSortedSlice(),
	})
}
