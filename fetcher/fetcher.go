package fetcher

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	// scraper alternatives
	// https://github.com/slotix/dataflowkit // requires docker instances
	// https://github.com/gocolly/colly
	// https://github.com/foolin/pagser
	// https://github.com/zhshch2002/goribot
	// https://github.com/andrewstuart/goq

	// https://github.com/geziyor/geziyor // has js render with local chrome
	// https://github.com/PuerkitoBio/gocrawl
	// https://github.com/PuerkitoBio/fetchbot
	// https://github.com/antchfx/antch

	// "github.com/ssgreg/stl" // resource locking
	"github.com/alitto/pond"
	"github.com/geziyor/geziyor"
	"github.com/geziyor/geziyor/cache"
	"github.com/geziyor/geziyor/client"
	"github.com/geziyor/geziyor/export"
	"github.com/geziyor/geziyor/middleware"
	"github.com/scytrin/eridanus"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

var maxWorkers = 10

// URLToResultsPath transforms a URL into a cache key with a hash component.
func URLToResultsPath(u string) string {
	sum := md5.Sum([]byte(u))
	path := filepath.Join("result_cache", fmt.Sprintf("%x", sum))
	if logrus.IsLevelEnabled(logrus.DebugLevel) {
		logrus.Infof("%s => %s", u, path)
	}
	return path
}

// URLToWebcachePath transforms a URL into a cache key with a hash component.
func URLToWebcachePath(u string) string {
	sum := md5.Sum([]byte(u))
	path := filepath.Join("web_cache", fmt.Sprintf("%x", sum))
	if logrus.IsLevelEnabled(logrus.DebugLevel) {
		logrus.Infof("%s => %s", u, path)
	}
	return path
}

// Fetcher fetches content from the internet.
type Fetcher struct {
	m     sync.Mutex
	s     eridanus.Storage
	p     *pond.WorkerPool
	gOpts *geziyor.Options
}

// NewFetcher returns a new fetcher instance.
func NewFetcher(ctx context.Context, s eridanus.Storage) (*Fetcher, error) {
	f := &Fetcher{s: s}

	f.gOpts = &geziyor.Options{
		Cache:                       &storageCache{f.s},
		CachePolicy:                 cache.RFC2616,
		ConcurrentRequests:          5,
		ConcurrentRequestsPerDomain: 1,
		ErrorFunc:                   f.errorFunc,
		Exporters:                   []export.Exporter{f},
		ParseFunc:                   f.parseFunc,
		RequestDelay:                1 * time.Microsecond,
		RequestDelayRandomize:       true,
		RequestMiddlewares:          []middleware.RequestProcessor{f},
		ResponseMiddlewares:         []middleware.ResponseProcessor{f},
		RobotsTxtDisabled:           true,
	}

	f.p = pond.New(maxWorkers, 0,
		pond.IdleTimeout(1*time.Second),
		// pond.MinWorkers(1),
		pond.PanicHandler(func(v interface{}) { logrus.Error(v) }),
		pond.Strategy(pond.Balanced()),
	)

	return f, nil
}

// Close shuts down the fetcher instance.
func (f *Fetcher) Close() error {
	f.m.Lock()
	defer f.m.Unlock()
	f.p.StopAndWait()
	return nil
}

// Get retrieves and parses a document from the internet.
func (f *Fetcher) Get(ctx context.Context, u string) (*eridanus.ParseResults, error) {
	f.m.Lock()
	defer f.m.Unlock()

	g := geziyor.NewGeziyor(f.gOpts)
	g.Client.Jar = f.s
	// g.Exports = make(chan interface{})
	g.Opt.StartURLs = []string{u}
	logrus.Infof("starting fetch from %s", u)
	g.Start()

	return f.Results(u)
}

// Results returns the parser results from storage.
func (f *Fetcher) Results(ru string) (*eridanus.ParseResults, error) {
	rPath := URLToResultsPath(ru)

	rc, err := f.s.GetData(rPath)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var results eridanus.ParseResults
	if err := yaml.NewDecoder(rc).Decode(&results); err != nil {
		return nil, err
	}

	return &results, nil
}

// Export handles exported data.
func (f *Fetcher) Export(exports chan interface{}) {
	for export := range exports {
		logrus.Debug(export)
	}
}

// ProcessRequest processes a request.
func (f *Fetcher) ProcessRequest(r *client.Request) {
}

// ProcessResponse processes a response.
func (f *Fetcher) ProcessResponse(r *client.Response) {
}

func (f *Fetcher) errorFunc(g *geziyor.Geziyor, r *client.Request, err error) {
	logrus.Debugf("ERROR: %s %v", r.URL, err)
	logrus.Error(err)
}

func (f *Fetcher) parseFunc(g *geziyor.Geziyor, r *client.Response) {
	ctx := context.Background()
	ru := r.Request.URL

	uc, nu, err := eridanus.Classify(ctx, ru, f.s.GetAllClassifiers())
	if err != nil {
		if r.IsHTML() || logrus.IsLevelEnabled(logrus.DebugLevel) {
			logrus.Error(err)
		}
		if r.IsHTML() {
			return
		}
	}

	results := &eridanus.ParseResults{}
	if !r.IsHTML() {
		idHash, err := f.s.PutContent(bytes.NewReader(r.Body))
		if err != nil {
			logrus.Error(err)
			return
		}
		tags, err := f.s.GetTags(idHash)
		if err != nil {
			logrus.Error(err)
			return
		}
		tags = append(tags, fmt.Sprintf("source:%s", ru))
		if err := f.s.PutTags(idHash, tags); err != nil {
			logrus.Error(err)
			return
		}
	} else {
		pResults, err := eridanus.Parse(ctx, string(r.Body), uc, f.s.GetAllParsers())
		if err != nil {
			logrus.Error(err)
			return
		}
		results.Results = pResults.GetResults()
	}

	// add source to results
	src := &eridanus.ParseResult{Type: eridanus.ParseResultType_SOURCE, Value: []string{ru.String()}}
	if nu != nil && ru.String() != nu.String() {
		src.Value = append(src.GetValue(), nu.String())
	}
	results.Results = append(results.Results, src)

	// process results
	for _, result := range results.GetResults() {
		switch result.GetType() {
		case eridanus.ParseResultType_CONTENT, eridanus.ParseResultType_NEXT, eridanus.ParseResultType_FOLLOW:
			for i, value := range result.GetValue() {
				value = r.JoinURL(value)
				result.Value[i] = value

				req, err := client.NewRequest("GET", value, nil)
				if err != nil {
					logrus.Error(err)
					continue
				}
				req.Header.Add("Referer", ru.String())
				g.Do(req, nil)
			}
		}
	}

	// export results
	g.Exports <- map[string]interface{}{
		"url":     ru.String(),
		"results": results.GetResults(),
	}

	// serialize results
	rBytes, err := yaml.Marshal(results)
	if err != nil {
		logrus.Error(err)
		return
	}

	// persist results
	rPath := URLToResultsPath(ru.String())
	if err := f.s.PutData(rPath, bytes.NewReader(rBytes)); err != nil {
		logrus.Error(err)
		return
	}
}
