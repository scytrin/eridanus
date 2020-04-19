package server

import (
	"bytes"
	"context"
	"encoding/xml"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/net/html"
)

type Fetcher struct {
	client *http.Client
}

func (f *Fetcher) redirectPolicyFunc(req *http.Request, via []*http.Request) error {
	return nil
}

func (f *Fetcher) Client() (*http.Client, error) {
	if f.client != nil {
		return f.client, nil
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	f.client = &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:       2,
			IdleConnTimeout:    5 * time.Second,
			DisableCompression: true,
		},
		CheckRedirect: f.redirectPolicyFunc,
		Jar:           jar,
	}

	return f.client, nil
}

func (f *Fetcher) Fetch(ctx context.Context, fetchURL *url.URL) error {
	c, err := f.Client()
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL.String(), nil)
	if err != nil {
		return err
	}

	res, err := c.Do(req)
	if err != nil {
		return err
	}

	log.Info(res)
	resBody, err := ioutil.ReadAll(res.Body)
	res.Body.Close()

	var i interface{}
	if err := xml.NewDecoder(bytes.NewReader(resBody)).Decode(&i); err != nil {
		return err
	}
	log.Info(i)

	doc, err := html.Parse(bytes.NewReader(resBody))
	if err != nil {
		return err
	}
	log.Info(doc)

	return nil
}
