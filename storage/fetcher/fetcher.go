package fetcher

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"github.com/golang/protobuf/proto"
	"github.com/scytrin/eridanus"
	"github.com/sirupsen/logrus"
)

const (
	webcacheNamespace  = "web_cache"
	webresultNamespace = "web_result"
)

type fetcherStorage struct {
	be eridanus.StorageBackend
	http.CookieJar
}

// NewFetcherStorage provides a new FetcherStorage.
func NewFetcherStorage(be eridanus.StorageBackend, cookies http.CookieJar) eridanus.FetcherStorage {
	return &fetcherStorage{be, cookies}
}

func (s *fetcherStorage) GetResults(u *url.URL) (*eridanus.ParseResults, error) {
	hsh := fmt.Sprintf("%x", md5.Sum([]byte(u.String())))
	rPath := fmt.Sprintf("%s/%s", webresultNamespace, hsh)
	var r eridanus.ParseResults
	rc, err := s.be.Get(rPath)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	d, err := ioutil.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	if err := proto.UnmarshalText(string(d), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *fetcherStorage) SetResults(u *url.URL, r *eridanus.ParseResults) error {
	hsh := fmt.Sprintf("%x", md5.Sum([]byte(u.String())))
	rPath := fmt.Sprintf("%s/%s", webresultNamespace, hsh)
	return s.be.Set(rPath, strings.NewReader(proto.CompactTextString(r)))
}

func (s *fetcherStorage) GetCached(u *url.URL) (*http.Response, error) {
	hsh := fmt.Sprintf("%x", md5.Sum([]byte(u.String())))
	cPath := fmt.Sprintf("%s/%s", webcacheNamespace, hsh)
	rc, err := s.be.Get(cPath)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	var reqSize int64
	if _, err := fmt.Fscanln(rc, &reqSize); err != nil {
		return nil, err
	}

	reqBuf := io.LimitReader(rc, int64(reqSize))
	req, err := http.ReadRequest(bufio.NewReader(reqBuf))
	if err != nil {
		return nil, err
	}

	var resSize int64
	if _, err := fmt.Fscanln(rc, &resSize); err != nil {
		return nil, err
	}

	resBuf := io.LimitReader(rc, int64(resSize))
	res, err := http.ReadResponse(bufio.NewReader(resBuf), req)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (s *fetcherStorage) SetCached(u *url.URL, res *http.Response) error {
	hsh := fmt.Sprintf("%x", md5.Sum([]byte(u.String())))
	cPath := fmt.Sprintf("%s/%s", webcacheNamespace, hsh)

	resBuf := bytes.NewBuffer(nil)
	res.Write(resBuf)

	reqBuf := bytes.NewBuffer(nil)
	if res.Request != nil {
		res.Request.Write(reqBuf)
	}

	buf := bytes.NewBuffer(nil)
	fmt.Fprintf(buf, "%d\n%s", reqBuf.Len(), reqBuf.Bytes())
	fmt.Fprintf(buf, "%d\n%s", resBuf.Len(), resBuf.Bytes())

	return s.be.Set(cPath, buf)
}

// Cookies implements the Cookies method of the http.CookieJar interface.
func (s *fetcherStorage) Cookies(u *url.URL) []*http.Cookie {
	cookies := s.Cookies(u)
	logrus.WithField("cookie", "get").WithField("url", u).Debug(cookies)
	return cookies
}

// SetCookies implements the SetCookies method of the http.CookieJar interface.
func (s *fetcherStorage) SetCookies(u *url.URL, cookies []*http.Cookie) {
	logrus.WithField("cookie", "set").WithField("url", u).Debug(cookies)
	s.SetCookies(u, cookies)
}
