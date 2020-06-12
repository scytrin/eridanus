package eridanus

import (
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
	"gopkg.in/xmlpath.v2"
)

// https://devhints.io/xpath

// ParsersFor returns Parser instances specified by the URLCLassifier.
func ParsersFor(ps []*Parser, c *URLClassifier) []*Parser {
	var keep []*Parser
	for _, p := range ps {
		for _, ru := range p.GetUrls() {
			u, err := url.Parse(ru)
			if err != nil {
				logrus.Warn("unable to parse example url: %s", ru)
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

// func parseJSON(pattern string, node *jsonquery.Node) ([]io.Reader, error) {
// 	var out []io.Reader

// 	nodes, err := jsonquery.QueryAll(node, pattern)
// 	if err != nil {
// 		return nil, err
// 	}
// 	for _, node := range nodes {
// 		value := node.InnerText()
// 		if value != "" {
// 			out = append(out, strings.NewReader(value))
// 		}
// 	}
// 	return out, nil
// }
