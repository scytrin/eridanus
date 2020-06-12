package eridanus

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
)

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
