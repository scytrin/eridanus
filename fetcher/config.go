package fetcher

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/gocolly/colly/v2"
	"github.com/sirupsen/logrus"
)

type XPathMatcher struct {
	Type, Selector  string
	PathMatch       string
	Prepend, Append string
}

func (m *XPathMatcher) String() string {
	return fmt.Sprintf("XPathMatcher{%s, %s, %s}",
		m.Type, m.Selector, m.PathMatch)
}

func (m *XPathMatcher) Match(u *url.URL) bool {
	match, err := regexp.MatchString(m.PathMatch, u.EscapedPath())
	if err != nil {
		logrus.Error(err)
		return false
	}
	return match
}

func (m *XPathMatcher) Install(c *colly.Collector, fc *Config, fr *fetch) {
	c.OnXML(m.Selector, func(e *colly.XMLElement) {
		url := e.Request.URL.String()
		value := e.Text
		log := fr.log.WithFields(logrus.Fields{
			"url":          url,
			"matcher":      m,
			"matcherValue": value,
		})

		if !fc.Match(e.Request.URL) {
			log.Debugf("no match %s to %s", fc.Name, e.Request.URL)
			return
		}

		if !m.Match(e.Request.URL) {
			log.Debugf("no match %v to %s", m, url)
			return
		}

		if m.Prepend != "" {
			value = m.Prepend + value
		}

		if m.Append != "" {
			value = value + m.Append
		}

		if m.Type == "tag" {
			value = strings.ToLower(value)
		}

		fr.Add(url, m.Type, value)
	})
}

type Config struct {
	Name     string
	Domains  []string
	Matchers []*XPathMatcher
}

func (fc *Config) String() string {
	return fmt.Sprintf("Config{%s, %s, [%d]XPathMatcher}",
		fc.Name, fc.Domains, len(fc.Matchers),
	)
}

func (fc *Config) Match(u *url.URL) bool {
	for _, domain := range fc.Domains {
		match, err := regexp.MatchString(domain, u.Hostname())
		if err != nil {
			logrus.Error(err)
			continue
		}
		if match {
			return true
		}
	}
	return false
}
