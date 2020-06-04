package eridanus

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
)

//go:generate stringer -type=StringMatcherType
type StringMatcherType int

func (t StringMatcherType) MarshalYAML() interface{} {
	return t.String()
}

func (t *StringMatcherType) UnmarshalYAML(unm func(interface{}) error) error {
	var i interface{}
	if err := unm(&i); err != nil {
		return err
	}
	switch v := i.(type) {
	case StringMatcherType:
		*t = v
	case int:
		*t = StringMatcherType(v)
	case string:
		for _, e := range StringMatcherTypes {
			if strings.ToLower(e.String()) == strings.ToLower(v) {
				*t = e
				return nil
			}
		}
		return fmt.Errorf("%s not a valid %T", v, t)
	}
	return nil
}

const (
	// Exact is an StringMatcherType enum.
	Exact StringMatcherType = iota
	// Regex is an StringMatcherType enum.
	Regex
)

// Array of StringMatcherType enum values.
var StringMatcherTypes = []StringMatcherType{
	Exact,
	Regex,
}

var stringMatcherFmt = regexp.MustCompile(`^(\w+:)?(\w+)(:\w+)?$`)

type StringMatcher struct {
	Type    StringMatcherType `yaml:",omitempty"`
	Value   string            `yaml:",omitempty"`
	Default string            `yaml:",omitempty"`
}

func (m StringMatcher) MarshalText() (text []byte, err error) {
	s := fmt.Sprintf("%s:%s:%s", m.Type, m.Value, m.Default)
	return bytes.Trim([]byte(s), " :"), nil
}

func (m *StringMatcher) UnmarshalText(text []byte) error {
	if r := stringMatcherFmt.FindSubmatch(text); r != nil {
		*m = StringMatcher{
			// Type: StringMatcherType(r[1]),
			Value:   string(r[2]),
			Default: string(r[3]),
		}
		return nil
	}
	return errors.New("incompatible format")
}

func (m *StringMatcher) Match(value string) bool {
	if value == "" {
		return false
	}
	if m.Value == "" {
		return true
	}
	switch m.Type {
	case Exact:
		return m.Value == value
	case Regex:
		match, err := regexp.MatchString(m.Value, value)
		if err != nil {
			logrus.Error(err)
			return false
		}
		return match
	default:
		logrus.Error("match has no defined type")
		return false
	}
}

type ParamMatcher struct {
	Key           string `yaml:",omitempty"`
	StringMatcher `yaml:",inline"`
}

//go:generate stringer -type=URLClassifierType
type URLClassifierType int

const (
	// File is an URLClassifierType enum.
	File URLClassifierType = iota
	// Post is an URLClassifierType enum.
	Post
	// List is an URLClassifierType enum.
	List
	// Watch is an URLClassifierType enum.
	Watch
)

// Array of URLClassifierType enum values.
var URLClassifierTypes = []URLClassifierType{
	File,
	Post,
	List,
	Watch,
}

func (t URLClassifierType) MarshalYAML() interface{} {
	return t.String()
}

func (t *URLClassifierType) UnmarshalYAML(unm func(interface{}) error) error {
	var i interface{}
	if err := unm(&i); err != nil {
		return err
	}
	switch v := i.(type) {
	case URLClassifierType:
		*t = v
	case int:
		*t = URLClassifierType(v)
	case string:
		for _, e := range URLClassifierTypes {
			if strings.ToLower(e.String()) == strings.ToLower(v) {
				*t = e
				return nil
			}
		}
		return fmt.Errorf("%s not a valid %T", v, t)
	}
	return nil
}

type URLClassifier struct {
	Name     string
	Type     URLClassifierType `yaml:",omitempty"`
	Priority int               `yaml:",omitempty"`

	Domain string           `yaml:",omitempty"`
	Path   []*StringMatcher `yaml:",omitempty"`
	Params []*ParamMatcher  `yaml:",omitempty"`

	Parsers []string `yaml:",omitempty"`

	UseHTTP          bool                   `yaml:",omitempty"`
	NoMatchSubdomain bool                   `yaml:",omitempty"`
	NoKeepSubdomain  bool                   `yaml:",omitempty"`
	Options          map[string]interface{} `yaml:",inline"`
}

func (uc *URLClassifier) normalizeDomain(ou *url.URL) (*url.URL, error) {
	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L2774
	u, err := url.Parse(ou.String())
	if err != nil {
		return nil, err
	}
	if uc.NoKeepSubdomain { // Somehow take synonym domains into account
		u.Host = uc.Domain
	}
	return u, nil
}

func (uc *URLClassifier) normalizePath(ou *url.URL) (*url.URL, error) {
	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L2795
	u, err := url.Parse(ou.String())
	if err != nil {
		return nil, err
	}
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
	return u, nil
}

func (uc *URLClassifier) normalizeQuery(ou *url.URL) (*url.URL, error) {
	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L2842
	u, err := url.Parse(ou.String())
	if err != nil {
		return nil, err
	}
	q := u.Query()
	pNames := make(map[string]struct{})
	for _, m := range uc.Params {
		pNames[m.Key] = struct{}{}
		if _, ok := q[m.Key]; !ok {
			if m.Default == "" {
				return nil, fmt.Errorf("no default for %s", m.Key)
			}
			q.Set(m.Key, m.Default)
		}
	}
	for k := range q {
		if _, ok := pNames[k]; !ok {
			q.Del(k)
		}
	}
	// Do I need to sort this to avoid random/hash ordering?
	u.RawQuery = q.Encode()
	return u, nil
}

func (uc *URLClassifier) Normalize(u *url.URL) (*url.URL, error) {
	u, err := uc.normalizeDomain(u)
	if err != nil {
		return nil, fmt.Errorf("unable to normalize %q: %v", u, err)
	}
	u, err = uc.normalizePath(u)
	if err != nil {
		return nil, fmt.Errorf("unable to normalize %q: %v", u, err)
	}
	u, err = uc.normalizeQuery(u)
	if err != nil {
		return nil, fmt.Errorf("unable to normalize %q: %v", u, err)
	}
	if u.Scheme != "https" && !uc.UseHTTP {
		u.Scheme = "https"
	}
	return u, nil
}

func (uc *URLClassifier) matchDomain(u *url.URL) bool {
	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L3313
	if uc.NoMatchSubdomain && u.Hostname() != uc.Domain {
		return false
	}
	if !strings.HasSuffix(u.Hostname(), uc.Domain) {
		return false // Somehow take synonym domains into account
	}
	return true
}

func (uc *URLClassifier) matchPath(u *url.URL) bool {
	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L3328
	pp := strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/")
	for i, m := range uc.Path {
		if i < len(pp) {
			if !m.Match(pp[i]) {
				return false
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

func (uc *URLClassifier) matchQuery(u *url.URL) bool {
	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L3358
	q := u.Query()
	for _, m := range uc.Params {
		if vs, ok := q[m.Key]; ok {
			for _, v := range vs {
				if !m.Match(v) {
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

func (uc *URLClassifier) Match(u *url.URL) bool {
	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L3309
	retval := uc.matchDomain(u) && uc.matchPath(u) && uc.matchQuery(u)
	logrus.Debug(uc.Name, " ", retval, " ", u)
	return retval
}

type URLClassifiers []*URLClassifier

func (cs URLClassifiers) For(u *url.URL) *URLClassifier {
	var keep *URLClassifier
	for _, c := range cs {
		if (keep == nil || keep.Priority < c.Priority) && c.Match(u) {
			keep = c
		}
	}
	return keep
}
