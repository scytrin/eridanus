// Package eridanus is an implementation of a content retrieval, storage, and
// categorizational system inspired by Hydrus Network.
package eridanus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/improbable-eng/go-httpwares/logging/logrus/ctxlogrus"
	"github.com/sirupsen/logrus"
	"gopkg.in/xmlpath.v2"
	// "github.com/mingrammer/commonregex"
)

var (
	// ErrItemNotFound is an identifiable error for "NotFound"
	ErrItemNotFound = errors.New("item not found")
	// ErrNilCommand is emitted when a nil *eridanis.Command is passed to nmh.Put or nmh.Run
	ErrNilCommand = errors.New("nil command provided")
	// ErrNilReader is emitted when a nil io.Reader is passed to nmh.Get or nmh.Run
	ErrNilReader = errors.New("nil reader provided")
	// ErrNilWriter is emitted when a nil io.Writer is passed to nmh.Put or nmh.Run
	ErrNilWriter = errors.New("nil writer provided")

	// ClassifierParserTypes specifies what parser results are expected per url classification.
	ClassifierParserTypes = map[URLClass_Class][]ParseResultType{
		URLClass_FILE: {ParseResultType_CONTENT},
		URLClass_POST: {ParseResultType_CONTENT, ParseResultType_TAG, ParseResultType_FOLLOW},
		URLClass_LIST: {ParseResultType_FOLLOW, ParseResultType_NEXT},
	}
)

// IDHash is a key for identifying an item.
type IDHash string

// GenerateIDHash returns a hashsum that will be used to identify the content.
func GenerateIDHash(r io.Reader) (IDHash, error) {
	h := sha256.New()
	io.Copy(h, r)
	return IDHash(fmt.Sprintf("%x", h.Sum(nil))), nil
}

func (h IDHash) String() string {
	return string(h)
}

// HexColor returns a variant of the hash for color specification.
func (h IDHash) HexColor() string {
	c, err := hex.DecodeString(string(h))
	if err != nil {
		logrus.Error(err)
		return ""
	}
	return "#" + hex.EncodeToString(c[:3])
}

// IDHashes are a collection of keys.
type IDHashes []IDHash

// ToSlice returns a string slice.
func (hs IDHashes) ToSlice() []string {
	var strs []string
	for _, v := range hs {
		strs = append(strs, v.String())
	}
	return strs
}

// Tag is a bit of metadata for an item.
type Tag string

func (t Tag) String() string {
	return string(t)
}

// Tags is a collection fo metadata.
type Tags []Tag

// TagsFromString parses a string composed of tags separated by commas.
func TagsFromString(tagsStr string) Tags {
	tags := Tags{}
	for _, tagStr := range strings.Split(tagsStr, ",") {
		tags = append(tags, Tag(tagStr))
	}
	sort.SliceStable(tags, func(i, j int) bool {
		return tags[i] < tags[j]
	})
	return tags.OmitDuplicates()
}

// OmitDuplicates keeps only the first instance of each same string in a slice.
func (ts Tags) OmitDuplicates() Tags {
	var out Tags
	seen := make(map[Tag]bool)
	for _, e := range ts {
		if !seen[e] {
			out = append(out, e)
			seen[e] = true
		}
	}
	return out
}

// ToSlice returns a string slice.
func (ts Tags) ToSlice() []string {
	var strs []string
	for _, v := range ts.OmitDuplicates() {
		strs = append(strs, v.String())
	}
	return strs
}

// String returns a string composed of tags separated by commas.
func (ts Tags) String() string {
	return strings.Join(ts.ToSlice(), ",")
}

// StorageBackend powers Storage.
type StorageBackend interface {
	GetRootPath() string
	RegisterOnClose(func() error)
	Close() error

	Keys(prefix string) ([]string, error)
	Set(key string, r io.Reader) error
	Has(key string) bool
	Get(key string) (io.ReadCloser, error)

	Delete(key string) error
	Import(srcPath, key string, move bool) error
}

// ClassesStorage stores classes.
type ClassesStorage interface {
	Names() ([]string, error)
	Put(*URLClass) error
	Has(string) bool
	Get(string) (*URLClass, error)
	For(*url.URL) (*URLClass, error)
}

// ParsersStorage stores parsers.
type ParsersStorage interface {
	Names() ([]string, error)
	Put(*Parser) error
	Has(string) bool
	Get(string) (*Parser, error)
	For(*URLClass) ([]*Parser, error)
}

// TagStorage stores tags.
type TagStorage interface {
	Hashes() (IDHashes, error)
	Put(IDHash, Tags) error
	Has(IDHash) bool
	Get(IDHash) (Tags, error)

	Find() (IDHashes, error)
}

// ContentStorage stores content.
type ContentStorage interface {
	Hashes() (IDHashes, error)
	Put(io.Reader) (IDHash, error)
	Has(IDHash) bool
	Get(IDHash) (io.ReadCloser, error)

	Thumbnail(IDHash) (io.ReadCloser, error)
}

// FetcherStorage stores web cache related data.
type FetcherStorage interface {
	http.CookieJar
	GetResults(*url.URL) (*ParseResults, error)
	SetResults(*url.URL, *ParseResults) error
	GetCached(*url.URL) (*http.Response, error)
	SetCached(*url.URL, *http.Response) error
}

// Storage manages data.
type Storage interface {
	StorageBackend
	ClassesStorage() ClassesStorage
	ParsersStorage() ParsersStorage
	TagStorage() TagStorage
	ContentStorage() ContentStorage
	FetcherStorage() FetcherStorage
}

// Fetcher acquires content.
type Fetcher interface {
	Close() error
	Get(context.Context, string) (*ParseResults, error)
	GetURL(context.Context, *url.URL) (*ParseResults, error)
	Results(string) (*ParseResults, error)
}

// RecoveryHandler allows for handling panics.
func RecoveryHandler(f func(error)) {
	switch rerr := recover().(type) {
	case error:
		f(rerr)
	case string:
		f(errors.New(rerr))
	default:
		f(fmt.Errorf("panicked: %v", rerr))
	}
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

		result := &ParseResult{
			Type:   p.GetType(),
			Parser: p.GetName(),
		}
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
	case StringMatcher_EXACT:
		return m.Value == value
	case StringMatcher_REGEX:
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
func Classify(u *url.URL, ucs []*URLClass) (*URLClass, *url.URL, error) {
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
	return nil, nil, fmt.Errorf("no classifier for %s", u)
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
			return nil, fmt.Errorf("domain mismatch: got %s, want %s", u.Hostname(), uc.GetDomain())
		}
		sDomain := "." + uc.Domain
		if !strings.HasSuffix(u.Hostname(), sDomain) {
			return nil, fmt.Errorf("subdomain mismatch: got %s, want %s", u.Hostname(), sDomain)
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
				return nil, fmt.Errorf("path segment mismatch: got %q, want %v", pathParts[i], m)
			}
			cp = append(cp, pathParts[i])
			continue
		}
		if m.Default != "" {
			cp = append(cp, m.Default)
			continue
		}
		return nil, fmt.Errorf("path length mismatch: got %d, want %d", len(pathParts), len(uc.GetPath()))
	}
	u.RawPath = "/" + strings.Join(cp, "/")

	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L2842
	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L3358
	q := u.Query()
	for k, m := range uc.GetQuery() {
		if vs, ok := q[k]; ok {
			for i, v := range vs {
				if !MatchStringMatcher(m, v) {
					return nil, fmt.Errorf("query param mismatch: %s[%d]=%q %v", k, i, v, m)
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
