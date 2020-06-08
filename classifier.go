package eridanus

//go:generate enumer -json -text -yaml -sql -type=StringMatcherType
//go:generate enumer -json -text -yaml -sql -type=URLClassifierType

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
)

// StringMatcherType indicates how a StringMatcher matches data.
type StringMatcherType int

const (
	// Exact is an StringMatcherType enum.
	Exact StringMatcherType = iota
	// Regex is an StringMatcherType enum.
	Regex
)

// StringMatcher matches a string.
type StringMatcher struct {
	Value   string            `yaml:",omitempty"`
	Default string            `yaml:",omitempty"`
	Type    StringMatcherType `yaml:",omitempty"`
}

func (m *StringMatcher) String() string {
	out := bytes.NewBuffer(nil)
	switch m.Type {
	case Exact:
		fmt.Fprintf(out, "%s", m.Value)
	case Regex:
		fmt.Fprintf(out, "{%s}", m.Value)
	}
	if m.Default != "" {
		fmt.Fprintf(out, ":%s", m.Default)
	}
	return out.String()
}

// MarshalText to satisfy encoding.TextMarshaler
func (m StringMatcher) MarshalText() (text []byte, err error) {
	return []byte(m.String()), nil
}

// UnmarshalText to satisfy encoding.TextUnmarshaler
func (m *StringMatcher) UnmarshalText(text []byte) error {
	nm := StringMatcher{Value: string(text)}
	if i := strings.LastIndex(nm.Value, ":"); i > -1 {
		nm.Default = nm.Value[i+1:]
		nm.Value = nm.Value[:i]
	}
	if strings.HasPrefix(nm.Value, "{") && strings.HasSuffix(nm.Value, "}") {
		nm.Type = Regex
		nm.Value = strings.Trim(nm.Value, "{}")
	}
	*m = nm
	return nil
}

// Match acts similarly to regexp.Regexp.Match.
func (m *StringMatcher) Match(value string) bool {
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
	case Exact:
		return m.Value == value
	case Regex:
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

// ParamMatcher is a StringMatcher for matching URL query parameters.
type ParamMatcher struct {
	Key           string `yaml:",omitempty"`
	StringMatcher `yaml:",inline"`
}

func (m *ParamMatcher) String() string {
	return fmt.Sprintf("%s=%s", m.Key, m.StringMatcher.String())
}

// MarshalText satisfies encoding.TextMarshaler
func (m ParamMatcher) MarshalText() (text []byte, err error) {
	return []byte(m.String()), nil
}

// UnmarshalText satisfies encoding.TextUnmarshaler
func (m *ParamMatcher) UnmarshalText(text []byte) error {
	parts := bytes.SplitN(text, []byte("="), 2)
	nm := ParamMatcher{Key: string(parts[0])}
	nm.StringMatcher.UnmarshalText(parts[1])
	*m = nm
	return nil
}

// URLClassifierType specifies the category of URL.
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

// URLClassifier determines the actions taken on a URL and it's content.
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

// Normalize returns a deterministic URL.
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

// Match comparses the given URL to the URLCLassifier's specification.
func (uc *URLClassifier) Match(u *url.URL) bool {
	// https://github.com/hydrusnetwork/hydrus/blob/1976391fd0a37c9caf607127b7a9a2d86a197d3c/hydrus/client/networking/ClientNetworkingDomain.py#L3309
	md, mp, mq := uc.matchDomain(u), uc.matchPath(u), uc.matchQuery(u)
	logrus.Debug(uc.Name, " ", []bool{md, mp, mq}, " ", u)
	return md && mp && mq
}

// URLClassifiers holds multiple URLClassifier instances.
type URLClassifiers []*URLClassifier

// Names provides the names of all included classifiers.
func (cs URLClassifiers) Names() []string {
	var names []string
	for _, c := range cs {
		names = append(names, c.Name)
	}
	return names
}

// For returns a URLClassifier instance appropriate for the URL.
func (cs URLClassifiers) For(u *url.URL) *URLClassifier {
	var keep *URLClassifier
	for _, c := range cs {
		if (keep == nil || keep.Priority < c.Priority) && c.Match(u) {
			keep = c
		}
	}
	return keep
}

// GetClassifier returns a URLClassifier specified by name.
func GetClassifier(name string) *URLClassifier {
	for _, v := range classes {
		if v.Name == name {
			return v
		}
	}
	return nil
}

// FindClassifier returns a URLClassifier most appropriate for the given URL.
func FindClassifier(u *url.URL) *URLClassifier {
	for _, v := range classes {
		if v.Match(u) {
			return v
		}
	}
	return nil
}
