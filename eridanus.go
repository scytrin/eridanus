// Package eridanus is an implementation of a content retrieval, storage, and
// categorizational system inspired by Hydrus Network.
package eridanus

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/improbable-eng/go-httpwares/logging/logrus/ctxlogrus"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/xmlpath.v2"
	// "github.com/mingrammer/commonregex"
)

var (
	// ErrItemNotFound is an identifiable error for "NotFound"
	ErrItemNotFound = errors.New("item not found")

	// ClassifierParserTypes specifies what parser results are expected per url classification.
	ClassifierParserTypes = map[URLClass_Class][]ParseResultType{
		URLClass_FILE: {ParseResultType_CONTENT},
		URLClass_POST: {ParseResultType_CONTENT, ParseResultType_TAG, ParseResultType_FOLLOW},
		URLClass_LIST: {ParseResultType_FOLLOW, ParseResultType_NEXT},
	}
)

// StorageBackend powers Storage.
type StorageBackend interface {
	Close() error
	Keys(prefix string) ([]string, error)
	PutData(key string, r io.Reader) error
	HasData(key string) bool
	GetData(key string) (io.ReadCloser, error)
	DeleteData(key string) error
}

// Storage manages content.
type Storage interface {
	StorageBackend
	http.CookieJar

	GetRootPath() string

	GetAllParsers() []*Parser
	AddParser(*Parser) error
	GetParserByName(string) (*Parser, error)

	GetAllClassifiers() []*URLClass
	AddClassifier(*URLClass) error
	GetClassifierByName(string) (*URLClass, error)

	PutTags(idHash string, tags []string) error
	GetTags(idHash string) ([]string, error)
	Find() ([]string, error)

	ContentKeys() ([]string, error)
	PutContent(r io.Reader) (idHash string, err error)
	GetContent(idHash string) (io.ReadCloser, error)

	GetThumbnail(idHash string) (io.ReadCloser, error)
}

// Fetcher acquires content.
type Fetcher interface {
	Close() error
	Get(context.Context, string) (*ParseResults, error)
	GetURL(context.Context, *url.URL) (*ParseResults, error)
	Results(string) (*ParseResults, error)
}

// RemoveDuplicateStrings keeps only the first instance of each same string in a slice.
func RemoveDuplicateStrings(in []string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, e := range in {
		if !seen[e] {
			out = append(out, e)
			seen[e] = true
		}
	}
	return out
}

// RecoveryHandler allows for handling panics.
func RecoveryHandler(f func(error)) {
	r := recover()
	if r == nil {
		return
	}
	logrus.Debugf("recovery: %v", r)

	var err error
	switch rerr := r.(type) {
	case error:
		err = rerr
	case string:
		err = errors.New(rerr)
	default:
		err = errors.Errorf("panicked: %v", rerr)
	}
	f(err)
}

// Parse applies provided parsers to the provided input.
func Parse(ctx context.Context, body string, uc *URLClass, ps []*Parser) (*ParseResults, error) {
	log := ctxlogrus.Extract(ctx).WithField("uc", uc.GetName())
	pts := ClassifierParserTypes[uc.GetClass()]

	var results ParseResults
	for _, p := range ps {
		log := log.WithField("p", p.GetName())
		var typeGood bool
		for _, pt := range pts {
			if p.GetType() != pt {
				continue
			}
			typeGood = true
			break
		}
		if !typeGood {
			// log.Warnf("type mismatch %q %q", uc.GetName(), p.GetName())
			continue
		}

		var urlGood bool
		for _, ru := range p.GetUrls() {
			u, err := url.Parse(ru)
			if err != nil {
				log.Warn(err)
				continue
			}
			if _, err := ApplyClassifier(uc, u); err != nil {
				// log.Warn(err)
				continue
			}
			urlGood = true
			break
		}
		if !urlGood {
			// log.Warnf("url mismatch %q %q", uc.GetName(), p.GetName())
			continue
		}

		result, err := ApplyParser(p, &ParseResult{Value: []string{body}})
		if err != nil {
			logrus.Warn(err)
			continue
		}
		if result != nil {
			result.Uclass = uc.GetName()
			log.Info(result)
			if len(result.GetValue()) > 0 {
				results.Results = append(results.GetResults(), result)
			}
		}
	}

	return &results, nil
}

// ApplyParser applies the provided parser to the provided input.
func ApplyParser(p *Parser, r *ParseResult) (*ParseResult, error) {
	log := logrus.WithField("p", p.GetName())
	out := r
	for i, op := range p.GetOperations() {
		log := log.WithField("op", i)
		if len(out.GetValue()) == 0 {
			return nil, nil
		}

		result := &ParseResult{Type: p.GetType(), Parser: p.GetName()}
		switch op.GetType() {
		case Parser_Operation_VALUE:
			result.Value = append(result.GetValue(), op.GetValue())
		case Parser_Operation_XPATH:
			for _, e := range out.GetValue() {
				results, err := parseHTML(op.GetValue(), strings.NewReader(e))
				if err != nil {
					return nil, err
				}
				result.Value = append(result.GetValue(), results...)
			}
		case Parser_Operation_REGEX:
			pattern, err := regexp.Compile(op.GetValue())
			if err != nil {
				return nil, err
			}
			for _, e := range out.GetValue() {
				for _, m := range pattern.FindAllString(e, -1) {
					result.Value = append(result.GetValue(), m)
				}
			}
		case Parser_Operation_PREFIX:
			for _, e := range out.GetValue() {
				result.Value = append(result.GetValue(), op.GetValue()+e)
			}
		case Parser_Operation_SUFFIX:
			for _, e := range out.GetValue() {
				result.Value = append(result.GetValue(), e+op.GetValue())
			}
		}
		log.Debug(result)
		out = result
	}

	if len(out.GetValue()) == 0 {
		return nil, nil
	}
	if out.GetType() == ParseResultType_TAG {
		for i, e := range out.GetValue() {
			out.Value[i] = strings.ToLower(e)
		}
	}
	return out, nil
}

func parseHTML(pattern string, html io.Reader) ([]string, error) {
	node, err := xmlpath.ParseHTML(html)
	if err != nil {
		return nil, err
	}

	xpath, err := xmlpath.Compile(pattern)
	if err != nil {
		return nil, err
	}

	if !xpath.Exists(node) {
		return nil, nil
	}

	var out []string
	for iter := xpath.Iter(node); iter.Next(); {
		value := iter.Node().String()
		if len(value) > 0 {
			out = append(out, value)
		}
	}
	return out, nil
}

// MatchStringMatcher acts similarly to regexp.Match.
func MatchStringMatcher(m *StringMatcher, value string) bool {
	if value == "" {
		return false
	}
	if m.Value == "" {
		return true
	}
	switch m.Type {
	default:
		logrus.Error("match has no defined type")
		return false
	case MatcherType_EXACT:
		return m.Value == value
	case MatcherType_REGEX:
		pattern, ok := map[string]string{
			"any":    `[^/]+`,
			"alpha":  `[A-Za-z]`,
			"alphas": `[A-Za-z]+`,
			"digit":  `[0-9]`,
			"digits": `[0-9]+`,
			"alnum":  `[A-Za-z0-9]`,
			"alnums": `[A-Za-z0-9]+`,
		}[m.Value]
		if !ok {
			pattern = m.Value
		}
		match, err := regexp.MatchString(pattern, value)
		if err != nil {
			logrus.Error(err)
			return false
		}
		return match
	}
}

// Classify returns the highest priority matching URLClass and the URL's normalized form.
func Classify(ctx context.Context, u *url.URL, ucs []*URLClass) (*URLClass, *url.URL, error) {
	var ucKeep *URLClass
	var urlKeep *url.URL
	for _, uc := range ucs {
		if ucKeep != nil && ucKeep.GetPriority() >= uc.GetPriority() {
			continue
		}
		nu, err := ApplyClassifier(uc, u)
		if err != nil {
			continue
		}
		ucKeep, urlKeep = uc, nu
	}
	if ucKeep != nil && urlKeep != nil {
		return ucKeep, urlKeep, nil
	}
	return nil, nil, errors.Errorf("no classifier for %s", u)
}

// ApplyClassifier applies a classifier to a URL, returning a normalized url or an error if the classifier doesn't apply.
func ApplyClassifier(uc *URLClass, ou *url.URL) (*url.URL, error) {
	u := url.URL(*ou)

	if u.Scheme != "https" && !uc.GetAllowHttp() {
		u.Scheme = "https"
	}

	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L3313
	// Somehow take synonym domains into account...
	// possibly allow specifying multiple domains
	if u.Hostname() != uc.GetDomain() {
		if !uc.MatchSubdomain {
			return nil, errors.Errorf("domain mismatch: got %s, want %s", u.Hostname(), uc.GetDomain())
		}
		sDomain := "." + uc.Domain
		if !strings.HasSuffix(u.Hostname(), sDomain) {
			return nil, errors.Errorf("subdomain mismatch: got %s, want %s", u.Hostname(), sDomain)
		}
		// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L2774
		if !uc.AllowSubdomain {
			u.Host = uc.Domain
		}
	}

	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L3328
	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L2795
	var cp []string
	pathParts := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	for i, m := range uc.GetPath() {
		if i < len(pathParts) {
			if !MatchStringMatcher(m, pathParts[i]) {
				return nil, errors.Errorf("path segment mismatch: got %q, want %v", pathParts[i], m)
			}
			cp = append(cp, pathParts[i])
			continue
		}
		if m.Default != "" {
			cp = append(cp, m.Default)
			continue
		}
		return nil, errors.Errorf("path length mismatch: got %d, want %d", len(pathParts), len(uc.GetPath()))
	}
	u.RawPath = "/" + strings.Join(cp, "/")

	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L2842
	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L3358
	q := u.Query()
	for k, m := range uc.GetQuery() {
		if vs, ok := q[k]; ok {
			for i, v := range vs {
				if !MatchStringMatcher(m, v) {
					return nil, errors.Errorf("query param mismatch: %s[%d]=%q %v", k, i, v, m)
				}
			}
			continue
		}
		if m.Default == "" {
			return nil, fmt.Errorf("no default for param %q", k)
		}
		q.Set(k, m.Default)
	}
	// remove any params remaining not specified in the classifier
	for k := range q {
		if _, ok := uc.GetQuery()[k]; !ok {
			q.Del(k)
		}
	}
	// Do I need to sort this to avoid random/hash ordering?
	u.RawQuery = q.Encode()

	return &u, nil
}
