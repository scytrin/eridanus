// Package eridanus is an implementation of a content retrieval, storage, and
// categorizational system inspired by Hydrus Network.
package eridanus

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/gocolly/colly/v2/extensions"
	"github.com/improbable-eng/go-httpwares/logging/logrus/ctxlogrus"
	"github.com/sirupsen/logrus"
	"go.chromium.org/luci/common/data/strpair"
	"gopkg.in/xmlpath.v2"
	"gopkg.in/yaml.v2"
)

//yaml.v2 https://play.golang.org/p/zt1Og9LIWNI
//yaml.v3 https://play.golang.org/p/H9WhcWSfJHT

const (
	classesBlobKey = "classes.yaml"
	parsersBlobKey = "parsers.yaml"
)

var (
	contentStore Storage
	parsers      Parsers
	classes      URLClassifiers

	// Collector is a colly.Collector for fetching content.
	Collector *colly.Collector
)

// IDHash returns a hashsum that will be used to identify the content.
func IDHash(r io.Reader) (string, error) {
	h := sha256.New()
	io.Copy(h, r)
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// HashToHexColor returns a value acceptable to use in specifying color.
func HashToHexColor(idHash string) string {
	i := big.NewInt(0)
	if _, ok := i.SetString(idHash, 16); !ok {
		return ""
	}
	return i.Mod(i, big.NewInt(0xffffff)).Text(16)
}

// Config holds configuration vlaues for eridanus.
type Config struct {
	LocalStorePath string
}

// Run starts the appropriate tasks for eridanus operations.
func Run(ctx context.Context, cfg Config) error {
	log := ctxlogrus.Extract(ctx)

	if err := ctx.Err(); err != nil {
		return err
	}

	if cfg.LocalStorePath == "" {
		return errors.New("LocalStorePath is not specified")
	}

	log = log.WithField("LocalStorePath", cfg.LocalStorePath)

	localContentStore, err := NewStorage(filepath.Join(cfg.LocalStorePath, "content"))
	if err != nil {
		return err
	}
	defer func() {
		if err := localContentStore.Close(); err != nil {
			log.Error(err)
		}
	}()
	contentStore = localContentStore

	cReader, err := contentStore.GetData(ctx, classesBlobKey, nil)
	if err != nil {
		if err != ErrBlobNotFound {
			return err
		}
	} else if err := yaml.NewDecoder(cReader).Decode(&classes); err != nil {
		return err
	}

	pReader, err := contentStore.GetData(ctx, parsersBlobKey, nil)
	if err != nil {
		if err != ErrBlobNotFound {
			return err
		}
	} else if err := yaml.NewDecoder(pReader).Decode(&parsers); err != nil {
		return err
	}

	Collector = colly.NewCollector()
	Collector.Async = true
	Collector.CheckHead = true
	Collector.IgnoreRobotsTxt = true
	Collector.MaxBodySize = 8e+7 // 10MB
	// Collector.CacheDir = filepath.Join(cfg.LocalStorePath, "colly")
	Collector.SetStorage(contentStore.AsCollyStorage())
	Collector.WithTransport(&http.Transport{
		MaxIdleConns:       3,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: true,
	})
	extensions.Referer(Collector)

	for _, us := range []string{
		// "https://pictures.hentai-foundry.com/f/Felox08/792226/Felox08-792226-Snowflake_-_Re_design.jpg",
		"https://www.hentai-foundry.com/pictures/user/Felox08/792226/Snowflake---Re-design",
		"https://www.hentai-foundry.com/pictures/user/Felox08/798105/Singularity",
		// "https://www.hentai-foundry.com/pictures/user/Felox08",
		// "https://www.hentai-foundry.com/user/Felox08/profile",
	} {
		u, err := url.Parse(us)
		if err != nil {
			log.Error(err)
			continue
		}
		results, err := Get(ctx, u)
		if err != nil {
			log.Error(err)
			continue
		}
		log.Debugf("%s\n%s", u.String(), strings.Join(results.Format(), "\n"))
	}

	cBytes, err := yaml.Marshal(classes)
	if err != nil {
		log.Error(err)
	} else if err := contentStore.PutData(ctx, classesBlobKey, bytes.NewReader(cBytes), nil); err != nil {
		return err
	}

	pBytes, err := yaml.Marshal(parsers)
	if err != nil {
		log.Error(err)
	} else if err := contentStore.PutData(ctx, parsersBlobKey, bytes.NewReader(pBytes), nil); err != nil {
		return err
	}

	return nil
}

type resultsCollector struct {
	ctx     context.Context
	results []strpair.Map
	result  strpair.Map
}

func (rc *resultsCollector) Get(u *url.URL) error {
	if rc.result == nil {
		rc.result = make(strpair.Map)
	}

	c := Collector.Clone()
	c.OnResponse(rc.onResponse)
	c.Request("GET", u.String(), nil, nil, nil)
	c.Wait()

	return nil
}

func (rc *resultsCollector) onResponse(res *colly.Response) {
	ru := res.Request.URL
	log := ctxlogrus.Extract(rc.ctx).WithField("url", ru.String())

	uc := FindClassifier(ru)
	if uc == nil {
		log.Errorf("no classification for %s", ru)
		return
	}

	u, err := uc.Normalize(ru)
	if err != nil {
		log.Error(err)
		return
	}

	ps := FindParsers(uc)
	if ps == nil {
		log.Errorf("no parsers for %s", u)
		return
	}

	urlHash, err := IDHash(strings.NewReader(u.String()))
	if err != nil {
		log.Error(err)
		return
	}

	if err := contentStore.PutData(rc.ctx,
		"/webcache/"+urlHash,
		bytes.NewReader(res.Body), nil); err != nil {
		log.Error(err)
	}

	node, err := xmlpath.ParseHTML(bytes.NewReader(res.Body))
	if err != nil {
		log.Error(err)
		return
	}

	results := strpair.ParseMap(rc.result[urlHash])
	results.Set("@", u.String())
	for _, p := range ps {
		if p.Value == "" {
			continue
		}
		r, err := p.ParseHTML(node)
		if err != nil {
			logrus.Warn(err)
			continue
		}
		for k, vs := range r {
			for _, v := range vs {
				if k == Follow.String() || k == Content.String() {
					v = res.Request.AbsoluteURL(v)
				}
				results.Add(k, v)
			}
		}
	}

	for _, link := range results[Content.String()] {
		if err := res.Request.Visit(link); err != nil {
			log.WithField("url", link).Error(err)
		}
	}

	for _, link := range results[Follow.String()] {
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

// Get retrieves a document from the internet.
func Get(ctx context.Context, u *url.URL) (strpair.Map, error) {
	log := ctxlogrus.Extract(ctx)
	rc := resultsCollector{
		ctx: ctxlogrus.ToContext(ctx, log),
	}
	rc.Get(u)

	return rc.result, nil
}
