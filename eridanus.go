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

	"github.com/sirupsen/logrus"
	"gocloud.dev/blob"
	"golang.org/x/xerrors"
	"gopkg.in/xmlpath.v2"
)

var (
	// ErrItemNotFound is an identifiable error for "NotFound"
	ErrItemNotFound = xerrors.New("item not found")
)

// Storage manages content.
type Storage interface {
	http.CookieJar

	Close() error

	PutData(ctx context.Context, path string, r io.Reader, opts *blob.WriterOptions) error
	GetData(ctx context.Context, path string, opts *blob.ReaderOptions) (io.Reader, error)

	// https://devhints.io/xpath
	GetAllParsers(context.Context) []*Parser
	AddParser(context.Context, *Parser) error
	GetParserByName(context.Context, string) (*Parser, error)
	FindParsers(context.Context, *URLClassifier) ([]*Parser, error)

	GetAllClassifiers(context.Context) []*URLClassifier
	AddClassifier(context.Context, *URLClassifier) error
	GetClassifierByName(context.Context, string) (*URLClassifier, error)
	FindClassifier(context.Context, *url.URL) (*URLClassifier, error)

	PutTags(idHash string, tags []string) error
	GetTags(idHash string) ([]string, error)
	Find() ([]string, error)

	ContentKeys() ([]string, error)
	PutContent(ctx context.Context, r io.Reader) (idHash string, err error)
	GetContent(idHash string) (io.Reader, error)

	GetThumbnail(idHash string) (io.Reader, error)
}

// RecoveryHandler allows for handling panics.
func RecoveryHandler(f func(error)) {
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

// Eridanus is an implementation of a content retrieval, storage, and
// categorizational system inspired by Hydrus Network.
type Eridanus struct {
	storage Storage
}

// New returns a new instance.
func New(ctx context.Context, s Storage) (*Eridanus, error) {
	return &Eridanus{storage: s}, nil
}

// Close closes open instances.
func (e *Eridanus) Close() error {
	return e.storage.Close()
}

// GetStorage returns the current storage instance.
func (e *Eridanus) GetStorage() Storage {
	return e.storage
}

// ParsersFor returns Parser instances matching the URLCLassifier.
func ParsersFor(ps []*Parser, c *URLClassifier) []*Parser {
	var keep []*Parser
	for _, p := range ps {
		for _, ru := range p.GetUrls() {
			u, err := url.Parse(ru)
			if err != nil {
				logrus.Warnf("unable to parse example url: %s", ru)
				continue
			}
			if ClassifierMatch(c, u) {
				keep = append(keep, p)
				break
			}
		}
	}
	return keep
}

// ParserByName returns a parser by name.
func ParserByName(ps []*Parser, name string) *Parser {
	for _, p := range ps {
		if p.GetName() == name {
			return p
		}
	}
	return nil
}

// Parse applies a parser to the provided input.
func Parse(p *Parser, rs []string) ([]string, error) {
	data := rs
	for _, op := range p.GetOperations() {
		result, err := parse(op, data)
		if err != nil {
			return nil, err
		}
		data = result
	}
	return data, nil
}

func parse(op *Parser_Operation, data []string) ([]string, error) {
	var out []string
	switch op.GetType() {
	case Parser_Operation_XPATH:
		for _, e := range data {
			results, err := parseHTML(op.GetValue(), strings.NewReader(e))
			if err != nil {
				return nil, err
			}
			out = append(out, results...)
		}
	case Parser_Operation_REGEX:
		pattern, err := regexp.Compile(op.GetValue())
		if err != nil {
			return nil, err
		}
		for _, e := range data {
			for _, m := range pattern.FindAllString(e, -1) {
				out = append(out, m)
			}
		}
	case Parser_Operation_PREFIX:
		for _, e := range data {
			out = append(out, op.GetValue()+e)
		}
	case Parser_Operation_SUFFIX:
		for _, e := range data {
			out = append(out, e+op.GetValue())
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

// MatchParamMatcher acts similarly to regexp.Match.
func MatchParamMatcher(m *ParamMatcher, key, value string) bool {
	sm := &StringMatcher{Type: m.Type, Default: m.Default, Value: m.Value}
	return key == m.GetKey() && MatchStringMatcher(sm, value)
}

// ClassifierMatch indicates if a URLCLassifier matches a URL.
func ClassifierMatch(uc *URLClassifier, u *url.URL) bool {
	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L3313
	// Somehow take synonym domains into account...
	// possibly allow specifying multiple domains
	if u.Hostname() != uc.Domain {
		if !uc.MatchSubdomain || !strings.HasSuffix(u.Hostname(), "."+uc.Domain) {
			return false
		}
	}

	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L3328
	pathParts := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	for i, m := range uc.Path {
		if i < len(pathParts) {
			if !MatchStringMatcher(m, pathParts[i]) {
				return false
			}
			continue
		}
		if m.Default != "" {
			continue
		}
		return false
	}

	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L3358
	q := u.Query()
	for _, m := range uc.GetQuery() {
		if vs, ok := q[m.Key]; ok {
			for _, v := range vs {
				if !MatchParamMatcher(m, m.Key, v) {
					return false
				}
			}
			continue
		}
		if m.Default != "" {
			continue
		}
		return false
	}

	return true
}

// ClassifierNormalize returns a normalized variant of the provided URL.
func ClassifierNormalize(uc *URLClassifier, ou *url.URL) (*url.URL, error) {
	u := url.URL(*ou)

	if u.Scheme != "https" && !uc.GetAllowHttp() {
		u.Scheme = "https"
	}

	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L2774
	if !uc.AllowSubdomain {
		u.Host = uc.Domain
	}

	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L2795
	pp := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	var cp []string
	for i, m := range uc.Path {
		if i < len(pp) {
			cp = append(cp, pp[i])
			continue
		}
		if m.Default != "" {
			cp = append(cp, m.Default)
			continue
		}
		return nil, fmt.Errorf("too short to normalize")
	}
	u.RawPath = "/" + strings.Join(cp, "/")

	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L2842
	q := u.Query()
	pNames := make(map[string]bool)
	for _, m := range uc.GetQuery() {
		pNames[m.Key] = true
		if _, ok := q[m.Key]; !ok {
			if m.Default == "" {
				return nil, fmt.Errorf("no default for %s", m.Key)
			}
			q.Set(m.Key, m.Default)
		}
	}
	for k := range q {
		if !pNames[k] {
			q.Del(k)
		}
	}
	// Do I need to sort this to avoid random/hash ordering?
	u.RawQuery = q.Encode()

	return &u, nil
}

// ClassifierFor returns a URLClassifier instance appropriate for the URL.
func ClassifierFor(cs []*URLClassifier, u *url.URL) *URLClassifier {
	var keep *URLClassifier
	for _, c := range cs {
		if (keep == nil || keep.Priority < c.Priority) && ClassifierMatch(c, u) {
			keep = c
		}
	}
	return keep
}

// ClassifierByName returns a URLClassifier specified by name.
func ClassifierByName(cs []*URLClassifier, name string) *URLClassifier {
	for _, v := range cs {
		if v.GetName() == name {
			return v
		}
	}
	return nil
}
