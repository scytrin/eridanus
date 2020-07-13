package fetcher

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
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

	"github.com/PuerkitoBio/fetchbot"
	"github.com/alitto/pond"
	"github.com/improbable-eng/go-httpwares/logging/logrus/ctxlogrus"
	"github.com/scytrin/eridanus"
	"github.com/sirupsen/logrus" // resource locking
	_ "golang.org/x/net/http2"   // http2 request and response parsing
	"golang.org/x/sync/semaphore"
)

var maxWorkers = 10

func buildClassParserMap(s eridanus.Storage) map[*eridanus.URLClass][]*eridanus.Parser {
	classes, err := s.ClassesStorage().GetAll()
	if err != nil {
		return nil
	}

	parsers, err := s.ParsersStorage().GetAll()
	if err != nil {
		return nil
	}

	d := make(map[*eridanus.URLClass][]*eridanus.Parser)
	for _, uc := range classes {
		for _, p := range parsers {
			var good bool
			for _, su := range p.GetUrls() {
				u, err := url.Parse(su)
				if err != nil {
					continue
				}
				if _, err := eridanus.ApplyClassifier(uc, u); err == nil {
					good = true
					break
				}
			}
			if good {
				d[uc] = append(d[uc], p)
			}
		}
	}
	return d
}

// Fetcher fetches content from the internet.
type Fetcher struct {
	m     *sync.RWMutex
	s     map[string]*semaphore.Weighted
	sLock map[string]*sync.Mutex

	d  map[*eridanus.URLClass][]*eridanus.Parser
	rt http.RoundTripper

	p *pond.WorkerPool
	c *http.Client

	fs eridanus.FetcherStorage
	cs eridanus.ClassesStorage
	ps eridanus.ParsersStorage
	ds eridanus.ContentStorage
	ts eridanus.TagStorage
}

// NewFetcher returns a new fetcher instance.
func NewFetcher(s eridanus.Storage) (*Fetcher, error) {
	f := &Fetcher{
		m: &sync.RWMutex{},
		s: map[string]*semaphore.Weighted{"": semaphore.NewWeighted(5)},

		rt: http.DefaultTransport,
		fs: s.FetcherStorage(),
		cs: s.ClassesStorage(),
		ps: s.ParsersStorage(),
		ds: s.ContentStorage(),
		ts: s.TagStorage(),
		d:  buildClassParserMap(s),
		p: pond.New(maxWorkers, 0,
			pond.IdleTimeout(1*time.Second),
			pond.PanicHandler(func(v interface{}) { logrus.Error(v) }),
			pond.Strategy(pond.Balanced()),
		),
	}

	f.c = &http.Client{
		Transport: f,
		Jar:       f.fs,
	}

	return f, nil
}

// Close shuts down the fetcher instance.
func (f *Fetcher) Close() error {
	f.m.Lock()
	defer f.m.Unlock()
	f.p.StopAndWait()
	return nil
}

// RoundTrip provides a caching RoundTripper.
func (f *Fetcher) RoundTrip(req *http.Request) (*http.Response, error) {
	f.m.Lock()
	defer f.m.Unlock()

	resCache, err := f.fs.GetCached(req.URL)
	if err != nil {
		logrus.Error(err)
	} else {
		return resCache, nil
	}

	if f.rt == nil {
		f.rt = http.DefaultTransport
	}

	res, err := f.rt.RoundTrip(req)
	if err := f.fs.SetCached(res.Request.URL, res); err != nil {
		logrus.Error(err)
	}

	return res, err
}

func (f *Fetcher) requestLock(ctx context.Context, u *url.URL) error {
	f.m.Lock()
	defer f.m.Unlock()

	if f.s[u.Hostname()] == nil {
		f.s[u.Hostname()] = semaphore.NewWeighted(1)
	}

	sLock := f.s[u.Hostname()]
	if err := sLock.Acquire(ctx, 1); err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		sLock.Release(1)
	}()

	return nil
}

type fbRequest struct {
	f   *Fetcher
	req *http.Request
	res eridanus.ParseResults
	err error
}

func (r *fbRequest) run() {
	ctx, cancel := context.WithCancel(r.req.Context())
	defer cancel()
	r.f.requestLock(ctx, r.req.URL)

	res, err := r.f.c.Do(r.req)
	if err != nil {
		r.err = err
		return
	}
	defer res.Body.Close()

	contentType := res.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "text/html") { // ParseHTML
		if err := r.f.parse(ctx, r.req, res); err != nil {
			r.err = err
			return
		}
	}
}

// Queue adds a url to be retrieved and processed.
func (f *Fetcher) Queue(req *http.Request) {
	f.p.Submit((&fbRequest{f: f, req: req}).run)
}

// QueueAndWait adds a url to be retrieved and processed in a synchronous manner.
func (f *Fetcher) QueueAndWait(req *http.Request) {
	f.p.SubmitAndWait((&fbRequest{f: f, req: req}).run)
}

func (f *Fetcher) parse(ctx context.Context, req *http.Request, res *http.Response) error {
	log := ctxlogrus.Extract(ctx)
	log.Info("parsing...")
	ru := res.Request.URL

	classes, err := f.cs.GetAll()
	if err != nil {
		return err
	}

	uc, nu, err := eridanus.Classify(ru, classes)
	if err != nil {
		return err
	}
	log = log.WithField("uc", uc.GetName()).WithField("nu", nu.String())

	if uc.GetClass() == eridanus.URLClass_IGNORE {
		log.Info("ignoring due to url class")
		return nil
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return err
	}

	var tags []string
	results := &eridanus.ParseResults{Results: []*eridanus.ParseResult{
		{Type: eridanus.ParseResultType_SOURCE, Value: []string{ru.String()}},
	}}
	for _, p := range f.d[uc] {
		log := log.WithField("p", p.GetName())
		pr := &eridanus.ParseResult{Value: []string{string(body)}}

		result, err := eridanus.ApplyParser(p, pr)
		if err != nil {
			log.Debug(err)
			continue
		}

		log.Debug(result)
		if result == nil {
			continue
		}

		switch result.GetType() {
		case eridanus.ParseResultType_TAG:
			tags = append(tags, result.GetValue()...)
		case eridanus.ParseResultType_CONTENT, eridanus.ParseResultType_NEXT, eridanus.ParseResultType_FOLLOW:
			for i, value := range result.GetValue() {
				nu, err := ru.Parse(value)
				if err != nil {
					log.Error(err)
					continue
				}
				result.Value[i] = nu.String()
			}
		}

		results.Results = append(results.GetResults(), result)
	}

	// persist results
	if err := f.fs.SetResults(ru, results); err != nil {
		return err
	}

	for _, result := range results.GetResults() {
		switch result.GetType() {
		case eridanus.ParseResultType_CONTENT, eridanus.ParseResultType_NEXT, eridanus.ParseResultType_FOLLOW:
			for _, value := range result.GetValue() {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, value, nil)
				if err != nil {
					log.Error(err)
					continue
				}
				f.Queue(req)
			}
		}
	}
	return nil
}

func (f *Fetcher) parseResponse(fbCtx *fetchbot.Context, res *http.Response, err error) {
	log := logrus.WithField("ru", res.Request.URL.String())
	if err != nil {
		log.WithError(err).Error(err)
		return
	}

	log.Info("parsing...")
	ru := res.Request.URL

	classes, err := f.cs.GetAll()
	if err != nil {
		log.Debug(err)
		return
	}

	uc, nu, err := eridanus.Classify(ru, classes)
	if err != nil {
		log.Debug(err)
		return
	}
	log = log.WithField("uc", uc.GetName()).WithField("nu", nu.String())

	if uc.GetClass() == eridanus.URLClass_IGNORE {
		log.Info("ignoring due to url class")
		return
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Error(err)
		return
	}

	var tags []string
	results := &eridanus.ParseResults{Results: []*eridanus.ParseResult{
		{Type: eridanus.ParseResultType_SOURCE, Value: []string{ru.String()}},
	}}
	for _, p := range f.d[uc] {
		log := log.WithField("p", p.GetName())
		pr := &eridanus.ParseResult{Value: []string{string(body)}}

		result, err := eridanus.ApplyParser(p, pr)
		if err != nil {
			log.Debug(err)
			continue
		}

		log.Debug(result)
		if result == nil {
			continue
		}

		switch result.GetType() {
		case eridanus.ParseResultType_TAG:
			tags = append(tags, result.GetValue()...)
		case eridanus.ParseResultType_CONTENT, eridanus.ParseResultType_NEXT, eridanus.ParseResultType_FOLLOW:
			for i, value := range result.GetValue() {
				nu, err := ru.Parse(value)
				if err != nil {
					log.Error(err)
					continue
				}
				result.Value[i] = nu.String()
			}
		}

		results.Results = append(results.GetResults(), result)
	}

	// persist results
	if err := f.fs.SetResults(ru, results); err != nil {
		log.Error(err)
		return
	}

	for _, result := range results.GetResults() {
		switch result.GetType() {
		case eridanus.ParseResultType_CONTENT, eridanus.ParseResultType_NEXT, eridanus.ParseResultType_FOLLOW:
			for _, value := range result.GetValue() {
				req, err := http.NewRequestWithContext(res.Request.Context(), http.MethodGet, value, nil)
				if err != nil {
					log.Error(err)
					continue
				}
				f.Queue(req)
			}
		}
	}
}

func (f *Fetcher) handleResponse(fbCtx *fetchbot.Context, res *http.Response, err error) {
	log := logrus.WithField("ru", res.Request.URL.String())
	if err != nil {
		log.WithError(err).Error(err)
		return
	}

	log.Info("handling...")
	ru := res.Request.URL

	classes, err := f.cs.GetAll()
	if err != nil {
		log.Debug(err)
		return
	}

	uc, nu, err := eridanus.Classify(ru, classes)
	if err != nil {
		log.WithError(err).Infof("no class: %s", ru)
	}

	if uc != nil {
		log = log.WithField("uc", uc.GetName()).WithField("nu", nu.String())
		if uc.GetClass() == eridanus.URLClass_IGNORE {
			log.Debug("ignoring due to url class")
			return
		}
	}

	idHash, err := f.ds.SetContent(res.Body)
	if err != nil {
		log.Error(err)
		return
	}
	log = logrus.WithField("h", idHash)

	tags, err := f.ts.GetTags(eridanus.IDHash(idHash))
	if err != nil {
		log.Error(err)
		return
	}

	tags = append(tags, eridanus.Tag(fmt.Sprintf("source:%s", ru)))

	if err := f.ts.SetTags(eridanus.IDHash(idHash), tags); err != nil {
		log.Error(err)
		return
	}
}
