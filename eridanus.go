// Package eridanus is an implementation of a content retrieval, storage, and
// categorizational system inspired by Hydrus Network.
package eridanus

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/gocolly/colly/v2/extensions"
	collyStorage "github.com/gocolly/colly/v2/storage"
	"github.com/improbable-eng/go-httpwares/logging/logrus/ctxlogrus"
	"github.com/scytrin/eridanus/idhash"
	"github.com/scytrin/eridanus/storage"
	"github.com/scytrin/eridanus/storage/importers"
	"go.chromium.org/luci/common/data/strpair"
	"gocloud.dev/blob"
	"golang.org/x/xerrors"
	"gopkg.in/yaml.v2"
)

// Storage manages content.
type Storage interface {
	CollyStorage() collyStorage.Storage
	CookieJar() http.CookieJar

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

//yaml.v2 https://play.golang.org/p/zt1Og9LIWNI
//yaml.v3 https://play.golang.org/p/H9WhcWSfJHT

const (
	contentNamespace = "content"
	classesBlobKey   = "classes.yaml"
	parsersBlobKey   = "parsers.yaml"
)

// NewCollector returns a new colly.Collector instance.
func NewCollector(ctx context.Context, storage Storage) (*colly.Collector, error) {
	if storage == nil {
		return nil, xerrors.New("Storage not available")
	}
	c := colly.NewCollector()
	c.Async = true
	c.CheckHead = true
	c.IgnoreRobotsTxt = true
	c.MaxBodySize = 8e+7 // 10MB
	c.SetStorage(storage.CollyStorage())
	c.WithTransport(&http.Transport{
		MaxIdleConns:       3,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: true,
	})
	extensions.Referer(c)
	return c, nil
}

// Eridanus is an implementation of a content retrieval, storage, and
// categorizational system inspired by Hydrus Network.
type Eridanus struct {
	storage   Storage
	parsers   []*Parser
	classes   []*URLClassifier
	collector *colly.Collector
}

// New returns a new instance.
func New(ctx context.Context, path string) (*Eridanus, error) {
	s, err := storage.NewStorage(ctx, path)
	if err != nil {
		return nil, err
	}

	c, err := NewCollector(ctx, s)
	if err != nil {
		return nil, err
	}

	e := &Eridanus{storage: s, collector: c}

	ctxlogrus.Extract(ctx).Infof("%#v", e)

	if err := e.loadClassesFromStorage(ctx); err != nil {
		return nil, err
	}

	if err := e.loadParsersFromStorage(ctx); err != nil {
		return nil, err
	}

	return e, nil
}

// Close closes open instances.
func (e *Eridanus) Close() error {
	ctx := context.Background()

	if err := e.saveClassesToStorage(ctx); err != nil {
		return err
	}

	if err := e.saveParsersToStorage(ctx); err != nil {
		return err
	}

	if err := e.storage.Close(); err != nil {
		return err
	}

	return nil
}

// Get retrieves a document from the internet.
func (e *Eridanus) Get(ctx context.Context, u *url.URL) (strpair.Map, error) {
	log := ctxlogrus.Extract(ctx)

	if c, err := importers.FetchChromeCookies(ctx, "", u); err != nil {
		log.Error(err)
	} else {
		log.Infof("%d cookies imported for %s", len(c), u)
		e.storage.CookieJar().SetCookies(u, c)
	}

	rc := resultsCollector{ctx: ctxlogrus.ToContext(ctx, log), e: e}
	rc.Get(u)

	return rc.result, nil
}

func (e *Eridanus) loadParsersFromStorage(ctx context.Context) error {
	r, err := e.storage.GetData(ctx, parsersBlobKey, nil)
	if err != nil {
		if err != storage.ErrBlobNotFound {
			return err
		}
		e.parsers = defaultConfig.GetParsers()
		return nil
	}
	return yaml.NewDecoder(r).Decode(&e.parsers)
}

func (e *Eridanus) loadClassesFromStorage(ctx context.Context) error {
	r, err := e.storage.GetData(ctx, classesBlobKey, nil)
	if err != nil {
		if err != storage.ErrBlobNotFound {
			return err
		}
		e.classes = defaultConfig.GetClasses()
		return nil
	}
	return yaml.NewDecoder(r).Decode(&e.classes)
}

func (e *Eridanus) saveParsersToStorage(ctx context.Context) error {
	b, err := yaml.Marshal(e.parsers)
	if err != nil {
		return err
	}
	return e.storage.PutData(ctx, parsersBlobKey, bytes.NewReader(b), nil)
}

func (e *Eridanus) saveClassesToStorage(ctx context.Context) error {
	b, err := yaml.Marshal(e.classes)
	if err != nil {
		return err
	}
	return e.storage.PutData(ctx, classesBlobKey, bytes.NewReader(b), nil)
}

type resultsCollector struct {
	ctx     context.Context
	results []strpair.Map
	result  strpair.Map
	e       *Eridanus
}

func (rc *resultsCollector) Get(u *url.URL) error {
	if rc.result == nil {
		rc.result = make(strpair.Map)
	}

	c := rc.e.collector.Clone()
	c.OnResponse(rc.onResponse)
	c.Request("GET", u.String(), nil, nil, nil)
	c.Wait()

	return nil
}

func (rc *resultsCollector) onResponse(res *colly.Response) {
	ru := res.Request.URL
	log := ctxlogrus.Extract(rc.ctx).WithField("url", ru.String())

	uc := ClassifierFor(rc.e.classes, ru)
	if uc == nil {
		log.Errorf("no classification for %s", ru)
		return
	}

	u, err := ClassifierNormalize(uc, ru)
	if err != nil {
		log.Error(err)
		return
	}

	urlHash, err := idhash.IDHash(strings.NewReader(u.String()))
	if err != nil {
		log.Error(err)
		return
	}

	results := strpair.ParseMap(rc.result[urlHash])
	results.Set("@", u.String())

	for _, p := range ParsersFor(rc.e.parsers, uc) {
		data, err := Parse(p, []string{string(res.Body)})
		if err != nil {
			log.Error(err)
			return
		}

		for i := range data {
			switch p.GetType() {
			case Parser_TAG:
				results.Add("tag", data[i])
			case Parser_CONTENT:
				results.Add("content", res.Request.AbsoluteURL(data[i]))
			case Parser_FOLLOW:
				results.Add("follow", res.Request.AbsoluteURL(data[i]))
			case Parser_SOURCE:
				results.Add("source", data[i])
			case Parser_MD5SUM:
				results.Add("md5sum", data[i])
			}
		}
	}
	log.Infof("%s: %v", u, results)

	for _, link := range results["follow"] {
		if err := res.Request.Visit(link); err != nil {
			log.WithField("url", link).Error(err)
		}
	}

	if _, ok := rc.result[urlHash]; ok {
		log.Infof("deleting %v", rc.result[urlHash])
		rc.result.Del(urlHash)
	}
	for _, pair := range results.Format() {
		rc.result.Add(urlHash, pair)
	}
	rc.results = append(rc.results, results)
}
