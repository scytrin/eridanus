package storage

import (
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	_ "gocloud.dev/blob/fileblob"        // for local buckets
	_ "gocloud.dev/blob/memblob"         // for memory buckets
	_ "gocloud.dev/docstore/memdocstore" // for memory docs
	"golang.org/x/net/idna"
)

type storageCookie struct {
	*http.Cookie
	CreatedAt  time.Time
	LastAccess time.Time
	seqNum     uint64
}

// id returns the domain;path;name triple of e as an id.
func (c *storageCookie) id() string {
	return fmt.Sprintf("%s;%s;%s", c.Domain, c.Path, c.Name)
}

type cookieJar struct {
	nextSeqNum uint64
	mux        sync.RWMutex
	psl        cookiejar.PublicSuffixList
	jar        map[string]map[string]storageCookie
}

// Cookies satisfies http.CookieJar.Cookies.
func (j *cookieJar) Cookies(u *url.URL) (cookies []*http.Cookie) {
	if u.Scheme != "http" && u.Scheme != "https" {
		return
	}
	key, err := j.cacheKey(u)
	if err != nil {
		logrus.Error(err)
		return
	}

	j.mux.RLock()
	defer j.mux.RUnlock()

	var selected []storageCookie
	for _, cookie := range j.jar[key] {
		selected = append(selected, cookie)
	}

	// sort according to RFC 6265 section 5.4 point 2: by longest
	// path and then by earliest creation time.
	sort.Slice(selected, func(i, j int) bool {
		s := selected
		if len(s[i].Path) != len(s[j].Path) {
			return len(s[i].Path) > len(s[j].Path)
		}
		if !s[i].CreatedAt.Equal(s[j].CreatedAt) {
			return s[i].CreatedAt.Before(s[j].CreatedAt)
		}
		return s[i].seqNum < s[j].seqNum
	})

	for _, cookie := range selected {
		cookies = append(cookies, &http.Cookie{
			Name:  cookie.Name,
			Value: cookie.Value,
		})
	}

	return
}

// SetCookies satisfies http.CookieJar.SetCookies.
func (j *cookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if len(cookies) == 0 {
		return
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return
	}
	key, err := j.cacheKey(u)
	if err != nil {
		logrus.Error(err)
		return
	}

	j.mux.Lock()
	defer j.mux.Unlock()

	if j.jar == nil {
		j.jar = make(map[string]map[string]storageCookie)
	}
	if j.jar[key] == nil {
		j.jar[key] = make(map[string]storageCookie)
	}
	for _, cookie := range cookies {
		sc := storageCookie{cookie, time.Now(), time.Now(), 0}
		id := sc.id()
		if ec, ok := j.jar[key][id]; ok {
			sc.CreatedAt = ec.CreatedAt
			sc.seqNum = ec.seqNum
		} else {
			sc.seqNum = j.nextSeqNum
			j.nextSeqNum++
		}
	}
}

func (j *cookieJar) cacheKey(u *url.URL) (string, error) {
	host, err := j.canonicalHost(u.Host)
	if err != nil {
		return "", err
	}

	if j.isIP(host) {
		return host, nil
	}

	var i int
	if j.psl == nil {
		i = strings.LastIndex(host, ".")
		if i <= 0 {
			return host, nil
		}
	} else {
		suffix := j.psl.PublicSuffix(host)
		if suffix == host {
			return host, nil
		}
		i = len(host) - len(suffix)
		if i <= 0 || host[i-1] != '.' {
			// The provided public suffix list psl is broken.
			// Storing cookies under host is a safe stopgap.
			return host, nil
		}
		// Only len(suffix) is used to determine the jar key from
		// here on, so it is okay if psl.PublicSuffix("www.buggy.psl")
		// returns "com" as the jar key is generated from host.
	}
	prevDot := strings.LastIndex(host[:i-1], ".")
	return host[prevDot+1:], nil
}

// isIP reports whether host is an IP address.
func (j *cookieJar) isIP(host string) bool {
	return net.ParseIP(host) != nil
}

// defaultPath returns the directory part of an URL's path according to
// RFC 6265 section 5.1.4.
func (j *cookieJar) defaultPath(path string) string {
	if len(path) == 0 || path[0] != '/' {
		return "/" // Path is empty or malformed.
	}

	i := strings.LastIndex(path, "/") // Path starts with "/", so i != -1.
	if i == 0 {
		return "/" // Path has the form "/abc".
	}
	return path[:i] // Path is either of form "/abc/xyz" or "/abc/xyz/".
}

// canonicalHost strips port from host if present and returns the canonicalized
// host name.
func (j *cookieJar) canonicalHost(host string) (string, error) {
	var err error
	host = strings.ToLower(host)
	if j.hasPort(host) {
		host, _, err = net.SplitHostPort(host)
		if err != nil {
			return "", err
		}
	}
	if strings.HasSuffix(host, ".") {
		// Strip trailing dot from fully qualified domain names.
		host = host[:len(host)-1]
	}
	return idna.ToASCII(host)
}

// hasPort reports whether host contains a port number. host may be a host
// name, an IPv4 or an IPv6 address.
func (j *cookieJar) hasPort(host string) bool {
	colons := strings.Count(host, ":")
	if colons == 0 {
		return false
	}
	if colons == 1 {
		return true
	}
	return host[0] == '[' && strings.Contains(host, "]:")
}
