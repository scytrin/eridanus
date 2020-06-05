// Package eridanus is an implementation of a content retrieval, storage, and
// categorizational system inspired by Hydrus Network.
package eridanus

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/corona10/goimagehash"
	"github.com/gocolly/colly/v2"
	"github.com/gocolly/colly/v2/debug"
	"github.com/gocolly/colly/v2/extensions"
	"github.com/improbable-eng/go-httpwares/logging/logrus/ctxlogrus"
	cookiejar "github.com/juju/persistent-cookiejar"
	"github.com/scytrin/eridanus/workerpool"
	"github.com/sirupsen/logrus"
	"go.chromium.org/luci/common/data/caching/cache"
	"go.chromium.org/luci/common/data/stringset"
	"go.chromium.org/luci/common/data/strpair"
	"go.chromium.org/luci/common/data/text/units"
	"go.chromium.org/luci/common/isolated"
	"golang.org/x/net/publicsuffix"
	"gopkg.in/xmlpath.v2"
	"gopkg.in/yaml.v2"
)

//yaml.v2 https://play.golang.org/p/zt1Og9LIWNI
//yaml.v3 https://play.golang.org/p/H9WhcWSfJHT

type key int

const (
	serverKey key = iota
)

var (
	// Collector is a colly.Collector for fetching content.
	Collector *colly.Collector
	// Client is used by Collector.
	Client *http.Client

	initLock    sync.Mutex
	initDone    bool
	maxWorkers  = 50
	persistDir  = ""
	siteConfigs SiteConfigs

	contentStore         cache.Cache
	contentStoreNS       = "sha256-content"
	contentStorePolicies = cache.Policies{
		MaxItems:     1e10,
		MinFreeSpace: units.Size(8e+9),
	}
)

// Config holds configuration vlaues for eridanus.
type Config struct {
	LocalStorePath string
}

// Run starts the appropriate tasks for eridanus operations.
func Run(ctx context.Context, cfg Config) error {
	initLock.Lock()
	defer initLock.Unlock()
	log := ctxlogrus.Extract(ctx)
	if !initDone {
		if cfg.LocalStorePath == "" {
			return errors.New("LocalStorePath is not specified")
		}

		persistDir = cfg.LocalStorePath
		log = log.WithField("persistDir", persistDir)

		localContentStore, err := cache.NewDisk(
			contentStorePolicies,
			filepath.Join(persistDir, "content"),
			contentStoreNS)
		if err != nil {
			return err
		}
		contentStore = localContentStore

		jar, err := cookiejar.New(&cookiejar.Options{
			PublicSuffixList: publicsuffix.List,
			Filename:         filepath.Join(persistDir, "cookies"),
		})
		if err != nil {
			return err
		}

		Client = &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:       10,
				IdleConnTimeout:    30 * time.Second,
				DisableCompression: true,
			},
			Jar: jar,
		}

		o := []colly.CollectorOption{
			colly.Async(true),
			colly.IgnoreRobotsTxt(),
			colly.MaxBodySize(8e+7), // 10MB
			colly.MaxDepth(3),
			colly.CacheDir(filepath.Join(persistDir, "colly")),
		}
		if false {
			o = append(o, colly.Debugger(new(debug.LogDebugger)))
		}
		Collector = colly.NewCollector(o...)
		Collector.CheckHead = true
		Collector.SetClient(Client)
		extensions.Referer(Collector)
		extensions.RandomUserAgent(Collector)

		if err := filepath.Walk(filepath.Join(persistDir, "sites"), func(p string, i os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if i.IsDir() {
				return nil
			}
			f, err := os.Open(p)
			if err != nil {
				log.Error(err)
				return nil
			}
			var siteConfig SiteConfig
			if err := yaml.NewDecoder(f).Decode(&siteConfig); err != nil {
				log.Error(err)
				return nil
			}
			siteConfigs = append(siteConfigs, &siteConfig)
			return nil
		}); err != nil {
			return err
		}
		initDone = true
	}

	yr, _ := yaml.Marshal(siteConfigs)
	log.Info(string(yr))

	return nil
}

// Save persists configuration to disk.
func Save(ctx context.Context) error {
	if Client != nil && Client.Jar != nil {
		if jar, ok := Client.Jar.(*cookiejar.Jar); ok && jar != nil {
			if err := jar.Save(); err != nil {
				return err
			}
		}
	}

	return nil
}

// Get retrieves a document from the internet.
func Get(ctx context.Context, u *url.URL) (strpair.Map, error) {
	log := ctxlogrus.Extract(ctx).WithFields(logrus.Fields{
		"url": u.String(),
	})

	c := siteConfigs.For(u)
	if c == nil {
		return nil, fmt.Errorf("no site config for %s", u)
	}

	uc := c.FindClassifier(u)
	if uc == nil {
		return nil, fmt.Errorf("no classification for %s", u)
	}

	nu, err := uc.Normalize(u)
	if err != nil {
		return nil, err
	}

	ps := c.FindParsers(uc)
	if ps == nil {
		return nil, fmt.Errorf("no parsers for %s", u)
	}

	allResults := make(strpair.Map)
	collector := Collector.Clone()
	collector.OnScraped(func(res *colly.Response) {
		node, err := xmlpath.ParseHTML(res.Request.Body)
		if err != nil {
			log.Error(err)
			return
		}

		results := make(strpair.Map)
		for _, pName := range uc.Parsers {
			p := c.GetParser(pName)
			r, err := p.ParseHTML(node)
			if err != nil {
				logrus.Warn(err)
				continue
			}
			for k, vs := range r {
				for _, v := range vs {
					results.Add(k, v)
					allResults.Add(k, v)
				}
			}
		}

		for _, link := range results[Follow.String()] {
			if err := res.Request.Visit(link); err != nil {
				log.Error(err)
			}
		}
	})

	if err := collector.Visit(nu.String()); err != nil {
		return nil, err
	}

	collector.Wait()

	return allResults, nil
}

// IDHash returns a hashsum that will be used to identify the content.
func IDHash(r io.Reader) (isolated.HexDigest, error) {
	return isolated.Hash(isolated.GetHash(contentStoreNS), r)
}

type writeCount int

func (c *writeCount) Write(p []byte) (n int, err error) {
	*c = writeCount(int(*c) + len(p))
	return len(p), nil
}

// GeneratePHashTags returns tags derived from the content oriented towards duplicate detection.
func GeneratePHashTags(img image.Image) (tags []string, err error) {
	defer func() {
		if rerr, ok := recover().(error); rerr != nil && ok {
			logrus.Error(rerr)
			tags = nil
			err = rerr
		}
	}()
	hsh, err := goimagehash.PerceptionHash(img)
	if err != nil {
		return nil, err
	}
	if hsh.GetHash() > 0 {
		tags = append(tags, fmt.Sprintf("phash:%s", hsh.ToString()))
	}

	return tags, nil
}

// ContentDerivedTags returns tags related to the content's format.
func ContentDerivedTags(idHash isolated.HexDigest) ([]string, error) {
	rc, err := contentStore.Read(idHash)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var c writeCount
	r := io.TeeReader(rc, &c)

	img, imgFormat, err := image.Decode(r)
	if err != nil {
		return nil, err
	}

	tags, err := GeneratePHashTags(img)
	if err != nil {
		logrus.Warnf("%s GeneratePHash: %v", idHash, err)
	}

	tags = append(tags,
		fmt.Sprintf("format:%s", imgFormat),
		fmt.Sprintf("filesize:%d", c),
		fmt.Sprintf("dimensions:%dx%d", img.Bounds().Size().X, img.Bounds().Size().Y),
	)

	return tags, nil
}

// AddTags optionally creates a .txt file containing tags.
func AddTags(idHash isolated.HexDigest, tags ...string) error {
	tagFilePath := "" // FIXME
	tagSet := stringset.NewFromSlice(tags...)

	tagFile, err := os.Open(tagFilePath)
	if err != nil {
		logrus.Warning(err)
	} else {
		for s := bufio.NewScanner(tagFile); s.Scan(); {
			tagSet.Add(s.Text())
		}
	}

	data := []byte(strings.Join(tagSet.ToSortedSlice(), "\n"))
	return ioutil.WriteFile(tagFilePath, data, 0644)
}

// IngestFunc receives content as an io.Reader and adds it to storage.
type IngestFunc func(context.Context, io.Reader, ...string) (string, error)

// Ingest implements IngestFunc.
func Ingest(ctx context.Context, r io.Reader, tags ...string) (isolated.HexDigest, error) {
	idHash, err := IDHash(strings.NewReader("meeeeeh"))
	if err != nil {
		return idHash, err
	}
	if err := contentStore.Add(idHash, r); err != nil {
		return idHash, err
	}
	cdTags, err := ContentDerivedTags(idHash)
	if err != nil {
		return idHash, err
	}
	tags = append(tags, cdTags...)
	if err := AddTags(idHash, tags...); err != nil {
		return idHash, err
	}
	return idHash, nil
}

// Import ingests content present on a local disk.
func Import(ctx context.Context, path string, ingest IngestFunc) error {
	log := ctxlogrus.Extract(ctx).WithField("importsDir", path)

	dirs, err := filepath.Glob(path)
	if err != nil {
		return err
	}

	pool := workerpool.NewPool(10)
	defer pool.Close()

	walkStart := time.Now()
	for _, dir := range dirs {
		if err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				log.Warn(walkErr)
				return walkErr
			}

			if info.IsDir() {
				return nil
			}

			pool.Do(ctx, func(ctx context.Context) {
				f, err := os.Open(path)
				if err != nil {
					log.Error(err)
					return
				}
				defer f.Close()

				idHash, err := ingest(ctx, f,
					fmt.Sprint("source:import"),
					fmt.Sprintf("filename:%s", filepath.Base(path)))
				if err != nil {
					log.Error(err)
				}

				log.Infof("%s => %s", path, idHash)
			})

			return nil
		}); err != nil {
			log.Error(err)
		}
	}
	log.Infof("%s -- walking %s", time.Now().Sub(walkStart), path)

	return nil
}

// SiteConfig is a container for site specific configuration.
type SiteConfig struct {
	Label       string
	Domain      string
	Parsers     ParserDefinitions
	Classifiers URLClassifiers
	// Generators  QueryDefinitions
}

// GetParser returns a parser from the SiteConfig by name.
func (c *SiteConfig) GetParser(name string) *ParserDefinition {
	for _, v := range c.Parsers {
		if v.Name == name {
			return v
		}
	}
	return nil
}

// FindParsers returns parsers from the SiteConfig specified by the provided URLClassifier.
func (c *SiteConfig) FindParsers(uc *URLClassifier) ParserDefinitions {
	var ps ParserDefinitions
	for _, name := range uc.Parsers {
		p := c.GetParser(name)
		if p == nil {
			logrus.Errorf("parser %s not found", name)
			continue
		}
		ps = append(ps, p)
	}
	return ps
}

// GetClassifier returns a URLClassifier specified by name.
func (c *SiteConfig) GetClassifier(name string) *URLClassifier {
	for _, v := range c.Classifiers {
		if v.Name == name {
			return v
		}
	}
	return nil
}

// FindClassifier returns a URLClassifier most appropriate for the given URL.
func (c *SiteConfig) FindClassifier(u *url.URL) *URLClassifier {
	for _, v := range c.Classifiers {
		if v.Match(u) {
			return v
		}
	}
	return nil
}

// SiteConfigs holds multiple SiteConfig instances.
type SiteConfigs []*SiteConfig

// For returns a SiteConfig instance appropriate for the URL.
func (cs SiteConfigs) For(u *url.URL) *SiteConfig {
	for _, c := range cs {
		if uc := c.FindClassifier(u); uc != nil {
			return c
		}
	}
	return nil
}
