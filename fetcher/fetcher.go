package fetcher

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gocolly/colly"
	"github.com/gocolly/colly/extensions"
	"github.com/improbable-eng/go-httpwares/logging/logrus/ctxlogrus"
	"github.com/scytrin/eridanus"
	"github.com/scytrin/eridanus/idhash"
	"github.com/scytrin/eridanus/storage/importers"
	"go.chromium.org/luci/common/data/strpair"
	"golang.org/x/xerrors"
)

// NewCollector returns a new colly.Collector instance.
func NewCollector(ctx context.Context, s eridanus.Storage) (*colly.Collector, error) {
	if s == nil {
		return nil, xerrors.New("Storage not available")
	}
	c := colly.NewCollector()
	c.Async = true
	c.CheckHead = true
	c.IgnoreRobotsTxt = true
	c.MaxBodySize = 8e+7 // 10MB
	if err := c.SetStorage(NewCollyStorage(s)); err != nil {
		return nil, err
	}
	c.WithTransport(&http.Transport{
		MaxIdleConns:       3,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: true,
	})
	extensions.Referer(c)
	return c, nil
}

// Fetcher fetches content from the internet.
type Fetcher struct {
	collector *colly.Collector
	s         eridanus.Storage
}

// NewFetcher retrns a new fetcher instance.
func NewFetcher(ctx context.Context, s eridanus.Storage) (*Fetcher, error) {
	c, err := NewCollector(ctx, s)
	if err != nil {
		return nil, err
	}

	return &Fetcher{collector: c, s: s}, nil
}

// Close shuts down the fetcher instance.
func (f *Fetcher) Close() error {
	return nil
}

// Get retrieves a document from the internet.
func (f *Fetcher) Get(ctx context.Context, u *url.URL) (strpair.Map, error) {
	log := ctxlogrus.Extract(ctx)

	if c, err := importers.FetchChromeCookies(ctx, "", u); err != nil {
		log.Error(err)
	} else {
		log.Infof("%d cookies imported for %s", len(c), u)
		f.s.SetCookies(u, c)
	}

	rc := resultsCollector{ctx: ctxlogrus.ToContext(ctx, log), f: f}
	rc.Get(u)

	return rc.result, nil
}

type resultsCollector struct {
	ctx     context.Context
	results []strpair.Map
	result  strpair.Map
	f       *Fetcher
}

func (rc *resultsCollector) Get(u *url.URL) error {
	if rc.result == nil {
		rc.result = make(strpair.Map)
	}

	c := rc.f.collector.Clone()
	c.OnResponse(rc.onResponse)
	c.Request("GET", u.String(), nil, nil, nil)
	c.Wait()

	return nil
}

func (rc *resultsCollector) onResponse(res *colly.Response) {
	ru := res.Request.URL
	log := ctxlogrus.Extract(rc.ctx).WithField("url", ru.String())

	classes := rc.f.s.GetAllClassifiers(rc.ctx)
	uc := eridanus.ClassifierFor(classes, ru)
	if uc == nil {
		log.Errorf("no classification for %s", ru)
		return
	}

	u, err := eridanus.ClassifierNormalize(uc, ru)
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

	parsers := rc.f.s.GetAllParsers(rc.ctx)
	for _, p := range eridanus.ParsersFor(parsers, uc) {
		data, err := eridanus.Parse(p, []string{string(res.Body)})
		if err != nil {
			log.Error(err)
			return
		}

		for i := range data {
			switch p.GetType() {
			case eridanus.Parser_TAG:
				results.Add("tag", data[i])
			case eridanus.Parser_CONTENT:
				results.Add("content", res.Request.AbsoluteURL(data[i]))
			case eridanus.Parser_FOLLOW:
				results.Add("follow", res.Request.AbsoluteURL(data[i]))
			case eridanus.Parser_SOURCE:
				results.Add("source", data[i])
			case eridanus.Parser_MD5SUM:
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
