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

const (
	cacheSize  = 1e6
	queueLimit = 1e3
	tmbX, tmbY = 150, 150
)

var (
	// ErrBlobNotFound is an identifiable error for "NotFound"
	ErrBlobNotFound = xerrors.New("not found")
)

type storageItem struct {
	IDHash string
	Tags   []string
}

// Storage provides a default implementation of eridanus.Storage.
type Storage struct {
	mux *sync.RWMutex

	rootPath     string
	cookieJar    http.CookieJar
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
		collyVisited: make(map[uint64]bool),
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

	s.cookieJar, err = NewCookieJar(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		return nil, err
	}

	return s, nil
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

// Close persists data to disk, then closes documents and buckets.
func (s *Storage) Close() error {
	s.mux.Lock()
	defer s.mux.Unlock()
	ctx := context.Background()

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

// CookieJar provides a storage consistent http.CookieJar.
func (s *Storage) CookieJar() http.CookieJar {
	return s.cookieJar
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

	var err error
	switch rerr := r.(type) {
	case error:
		err = rerr
	case string:
		err = xerrors.New(rerr)
	default:
		err = xerrors.Errorf("panicked: %v", rerr)
	}
	f(err)
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
		return xerrors.New("no phash generated")
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
