package fetcher

// import (
// 	"bytes"
// 	"context"
// 	"fmt"
// 	"io/ioutil"
// 	"net/http"
// 	"net/http/cookiejar"
// 	"net/url"
// 	"path"
// 	"strings"
// 	"sync"
// 	"time"

// 	"github.com/gocolly/colly/v2"
// 	"github.com/gocolly/colly/v2/debug"
// 	"github.com/gocolly/colly/v2/extensions"
// 	"github.com/improbable-eng/go-httpwares/logging/logrus/ctxlogrus"
// 	"github.com/kr/pretty"
// 	"github.com/sirupsen/logrus"
// 	"gopkg.in/xmlpath.v2"
// 	"stadik.net/eridanus"
// )

// type key int

// const (
// 	fetchKey key = iota
// 	breadcrumbsKey
// )

// type FetchResult []string // tags and metadata

// func (fr FetchResult) ByPrefix(prefix string) []string {
// 	prefix = prefix + ":"
// 	var values []string
// 	for _, e := range fr {
// 		if strings.HasPrefix(e, prefix) {
// 			values = append(values, strings.TrimPrefix(e, prefix))
// 		}
// 	}
// 	return values
// }

// type Fetcher struct {
// 	PoolSize int
// 	CacheDir string
// 	Config   []*Config `yaml:",omitempty"`
// }

// func (f *Fetcher) buildCollector(id uint32) *colly.Collector {
// 	o := []colly.CollectorOption{
// 		// colly.AllowURLRevisit(),
// 		// colly.Async(true),
// 		colly.Debugger(new(debug.LogDebugger)),
// 		colly.IgnoreRobotsTxt(),
// 		colly.MaxBodySize(8e+7), // 10MB
// 		colly.MaxDepth(3),
// 		// colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/81.0.4044.122 Safari/537.36"),
// 	}
// 	if id > 0 {
// 		o = append(o, colly.ID(id))
// 	}
// 	// if f.CacheDir != "" {
// 	// 	o = append(o, colly.CacheDir(f.CacheDir))
// 	// }
// 	c := colly.NewCollector(o...)
// 	c.SetRequestTimeout(10 * time.Second)
// 	c.Limit(&colly.LimitRule{
// 		DomainGlob:  "*",
// 		Parallelism: 1,
// 		Delay:       1 * time.Second,
// 		RandomDelay: 5 * time.Second,
// 	})
// 	extensions.Referer(c)
// 	return c
// }

// func (f *Fetcher) I() eridanus.Fetcher {
// 	return f
// }

// func (f *Fetcher) MarshalYAML() (interface{}, error) {
// 	return f, nil
// }

// func (f *Fetcher) UnmarshalYAML(unm func(interface{}) error) error {
// 	type alias Fetcher
// 	if err := unm((*alias)(f)); err != nil {
// 		return err
// 	}
// 	return nil
// }

// func (f *Fetcher) Fetch(ctx context.Context, s string) (eridanus.FetchResult, error) {
// 	c := f.buildCollector(0)
// 	fr := newFetch(ctx, c)
// 	for _, fc := range f.Config {
// 		for _, m := range fc.Matchers {
// 			m.Install(c, fc, fr)
// 		}
// 	}
// 	if err := c.Visit(s); err != nil {
// 		return nil, err
// 	}
// 	c.Wait()
// 	return FetchResult(fr.GetAll("added")), nil
// }

// func (f *Fetcher) rawFetch(ctx context.Context, s string) (eridanus.FetchResult, error) {
// 	log := ctxlogrus.Extract(ctx)
// 	fr := newFetch(ctx, nil)
// 	jar, _ := cookiejar.New(nil)
// 	c := &http.Client{Jar: jar}

// 	res, err := c.Get(s)
// 	if err != nil {
// 		return nil, err
// 	}
// 	defer res.Body.Close()

// 	resBody, err := ioutil.ReadAll(res.Body)
// 	if err != nil {
// 		return nil, err
// 	}

// 	if !strings.Contains(res.Header.Get("Content-Type"), "text/html") {
// 		idHash, err := fr.srv.Ingest(context.Background(), bytes.NewReader(resBody),
// 			fmt.Sprint("source:upload"),
// 			fmt.Sprintf("filename:%s", path.Base(s)),
// 		)
// 		if err != nil {
// 			return nil, err
// 		}
// 		fr.Add(s, "added", idHash)
// 	} else {
// 		u, err := url.Parse(s)
// 		if err != nil {
// 			return nil, err
// 		}
// 		node, err := xmlpath.ParseHTML(bytes.NewReader(resBody))
// 		if err != nil {
// 			return nil, err
// 		}
// 		for _, config := range f.Config {
// 			if !config.Match(u) {
// 				continue
// 			}
// 			for _, matcher := range config.Matchers {
// 				if !matcher.Match(u) {
// 					continue
// 				}
// 				for iter := xmlpath.MustCompile(matcher.Selector).Iter(node); iter.Next(); {
// 					log.Info(iter.Node().String())
// 				}
// 			}
// 		}
// 	}
// 	return FetchResult(fr.GetAll("added")), nil
// }

// type fetch struct {
// 	c          *colly.Collector
// 	srv        eridanus.ServerI
// 	log        logrus.FieldLogger
// 	valuesLock *sync.Mutex
// 	values     map[string]map[string][]string
// }

// func newFetch(ctx context.Context, c *colly.Collector) *fetch {
// 	fr := &fetch{
// 		c:          c,
// 		srv:        eridanus.FromContext(ctx),
// 		log:        ctxlogrus.Extract(ctx),
// 		valuesLock: new(sync.Mutex),
// 		values:     make(map[string]map[string][]string),
// 	}
// 	c.OnRequest(fr.onRequest)
// 	c.OnResponse(fr.onResponse)
// 	c.OnScraped(fr.onScraped)
// 	c.OnError(fr.onError)
// 	return fr
// }

// func (f *fetch) Add(u, k, v string) {
// 	f.valuesLock.Lock()
// 	defer f.valuesLock.Unlock()
// 	if _, ok := f.values[u]; ok {
// 		f.values[u][k] = append(f.values[u][k], v)
// 		return
// 	}
// 	f.values[u] = map[string][]string{k: {v}}
// }

// func (f *fetch) GetKeys(u string) []string {
// 	f.valuesLock.Lock()
// 	defer f.valuesLock.Unlock()
// 	var ret []string
// 	for k := range f.values[u] {
// 		ret = append(ret, k)
// 	}
// 	return ret
// }

// func (f *fetch) GetAll(k string) []string {
// 	f.valuesLock.Lock()
// 	defer f.valuesLock.Unlock()
// 	var ret []string
// 	for _, values := range f.values {
// 		ret = append(ret, values[k]...)
// 	}
// 	return ret
// }

// func (f *fetch) Get(u, k string) []string {
// 	f.valuesLock.Lock()
// 	defer f.valuesLock.Unlock()
// 	return f.values[u][k]
// }

// func (f *fetch) onRequest(req *colly.Request) {
// 	log := f.log.WithField("url", req.URL.String())
// 	log.Debugf("%# v", pretty.Formatter(req.Ctx))
// 	log.Debugf("%# v", pretty.Formatter(req.Headers))
// }

// func (f *fetch) onResponse(res *colly.Response) {
// 	log := f.log.WithField("url", res.Request.URL.String())
// 	log.Debugf("%# v\n%s", pretty.Formatter(res.Headers), string(res.Body))

// 	if strings.Contains(res.Headers.Get("Content-Type"), "text/html") {
// 		return
// 	}

// 	if f.srv == nil {
// 		log.Error("server not found, unable to retain content")
// 		return
// 	}

// 	log.Infof("%# v", pretty.Formatter(f.values))
// 	log.Infof("%# v", pretty.Formatter(res.Ctx))

// 	idHash, err := f.srv.Ingest(context.Background(), bytes.NewReader(res.Body),
// 		fmt.Sprint("source:upload"),
// 		fmt.Sprintf("filename:%s", res.FileName()),
// 	)
// 	if err != nil {
// 		log.Error(err)
// 		return
// 	}

// 	f.Add(res.Request.URL.String(), "added", idHash)
// 	log.Infof("%# v", pretty.Formatter(f.values[res.Request.URL.String()]))
// }

// func (f *fetch) onScraped(res *colly.Response) {
// 	url := res.Request.URL.String()
// 	log := f.log.WithField("url", url)

// 	log.Infof("%# v", pretty.Formatter(res.Ctx))
// 	log.Infof("%# v", pretty.Formatter(f.values[url]))

// 	if !strings.Contains(res.Headers.Get("Content-Type"), "text/html") {
// 		return
// 	}

// 	for kind, values := range f.values[url] {
// 		for _, value := range values {
// 			switch kind {
// 			case "image", "follow", "consent":
// 				res.Request.Visit(value)
// 			}
// 		}
// 	}
// }

// func (f *fetch) onError(res *colly.Response, err error) {
// 	log := f.log.WithField("url", res.Request.URL.String())

// 	log.Error(err)
// }
