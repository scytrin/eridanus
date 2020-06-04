package fetcher

//
// import (
// 	"bytes"
// 	"context"
// 	"fmt"
// 	"net/url"
// 	"path"
// 	"strings"
// 	"sync"
// 	"time"
//
// 	"github.com/improbable-eng/go-httpwares/logging/logrus/ctxlogrus"
// 	"go.chromium.org/luci/common/data/stringset"
// )
//
// type toIngest struct {
// 	url     *url.URL
// 	content []byte
// 	tags    []string
// }
//
// type run struct {
// 	fetcher    *Fetcher
// 	slots      chan int
// 	seenLock   sync.Mutex
// 	seen       map[string]bool
// 	valuesLock sync.RWMutex
// 	values     map[string][]string
// 	ingestLock sync.RWMutex
// 	ingest     []toIngest
// }
//
// func (r *run) Wait() error {
// 	ticker := time.NewTicker(3 * time.Second)
// 	defer ticker.Stop()
// 	var count int
// 	for count < 5 {
// 		<-time.After(1 * time.Second)
// 		if len(r.slots) == cap(r.slots) {
// 			count++
// 			continue
// 		}
// 		count = 0
// 	}
// 	return nil
// }
//
// func (r *run) GetSeen() []string {
// 	var seen []string
// 	for k := range r.seen {
// 		seen = append(seen, k)
// 	}
// 	return seen
// }
//
// func (r *run) AddValues(u *url.URL, values []string) {
// 	r.valuesLock.Lock()
// 	defer r.valuesLock.Unlock()
// 	r.values[u.String()] = values
// }
//
// func (r *run) GetValues(u *url.URL) []string {
// 	r.valuesLock.RLock()
// 	defer r.valuesLock.RUnlock()
// 	return r.values[u.String()]
// }
//
// func (r *run) ToIngest() []toIngest {
// 	return r.ingest
// }
//
// func (r *run) unseen(u *url.URL) bool {
// 	r.seenLock.Lock()
// 	defer r.seenLock.Unlock()
// 	if !r.seen[u.String()] {
// 		r.seen[u.String()] = true
// 		return true
// 	}
// 	return false
// }
//
// func (r *run) Add(ctx context.Context, u *url.URL) {
// 	if !r.unseen(u) {
// 		return
// 	}
// 	crumbs := []string{u.String()}
// 	if old, ok := ctx.Value(breadcrumbsKey).([]string); ok {
// 		crumbs = append(crumbs, old...)
// 	}
// 	ctx = context.WithValue(ctx, breadcrumbsKey, crumbs)
// 	go r.fetch(ctx, u, <-r.slots)
// }
//
// func (r *run) fetch(ctx context.Context, u *url.URL, slot int) error {
// 	defer func() { r.slots <- slot }()
// 	log := ctxlogrus.Extract(ctx).WithField("fetch", u.String())
// 	ctx = ctxlogrus.ToContext(ctx, log)
//
// 	values, err := r.scrape(ctx, u)
// 	if err != nil {
// 		log.Error(err)
// 	}
// 	log.Debug(values)
// 	r.AddValues(u, values) // enables compileTags()
//
// 	// values := r.GetValues(u)
// 	for _, value := range values {
// 		if err := r.processValue(ctx, u, value); err != nil {
// 			log.Error(err)
// 		}
// 	}
// 	return nil
// }
//
// func (r *run) scrape(ctx context.Context, u *url.URL) ([]string, error) {
// 	content, contentType, err := r.fetcher.getContent(u)
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	if !strings.Contains(contentType, "text/html") {
// 		return nil, fmt.Errorf("unsupported content-type %s for %s", contentType, u)
// 	}
//
// 	configs := r.fetcher.configsFor(u)
// 	return r.fetcher.extractValues(bytes.NewReader(content), configs...)
// }
//
// func (r *run) processValue(ctx context.Context, u *url.URL, value string) error {
// 	log := ctxlogrus.Extract(ctx)
// 	r.ingestLock.RLock()
// 	defer r.ingestLock.RUnlock()
//
// 	sep := strings.Index(value, ":")
// 	if sep < 0 {
// 		return nil
// 	}
//
// 	kind, value := value[:sep], value[sep+1:]
// 	switch kind {
// 	case "follow":
// 		nu, err := u.Parse(value)
// 		if err != nil {
// 			return err
// 		}
// 		r.Add(ctx, nu)
//
// 	case "image":
// 		nu, err := u.Parse(value)
// 		if err != nil {
// 			return err
// 		}
// 		tags := append(r.compileTagsFromBreadcrumbs(ctx),
// 			fmt.Sprintf("source:%s", nu.String()),
// 			fmt.Sprintf("filename:%s", path.Base(nu.String())),
// 		)
// 		log.Debug(tags)
// 		content, _, err := r.fetcher.getContent(nu)
// 		if err != nil {
// 			return err
// 		}
// 		r.ingest = append(r.ingest, toIngest{nu, content, tags})
// 	}
// 	return nil
// }
//
// func (r *run) compileTagsFromBreadcrumbs(ctx context.Context) []string {
// 	r.valuesLock.RLock()
// 	defer r.valuesLock.RUnlock()
//
// 	tagSet := stringset.New(0)
// 	for _, urlString := range ctx.Value(breadcrumbsKey).([]string) {
// 		for _, value := range r.values[urlString] {
// 			if !strings.HasPrefix(value, "tag:") {
// 				continue
// 			}
// 			tagSet.Add(strings.TrimPrefix(value, "tag:"))
// 		}
// 	}
//
// 	tags := tagSet.ToSortedSlice()
// 	for i, tag := range tags {
// 		tags[i] = strings.ToLower(tag)
// 	}
//
// 	return tags
// }
