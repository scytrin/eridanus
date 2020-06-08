package eridanus

//go:generate enumer -json -text -yaml -sql -type=ParserOutputType
//go:generate enumer -json -text -yaml -sql -type=ParserOpType

import (
	"fmt"
	"net/url"

	"github.com/antchfx/jsonquery"
	"github.com/sirupsen/logrus"
	"go.chromium.org/luci/common/data/stringset"
	"go.chromium.org/luci/common/data/strpair"
	"gopkg.in/xmlpath.v2"
)

// https://devhints.io/xpath

// ParserOutputType specifies the type of result from a parser.
type ParserOutputType int

const (
	// Content is an ParserOutputType enum.
	Content ParserOutputType = iota
	// Tag is an ParserOutputType enum.
	Tag
	// Follow is an ParserOutputType enum.
	Follow
	// Title is an ParserOutputType enum.
	Title
	// Source is an ParserOutputType enum.
	Source
	// MD5Hash is an ParserOutputType enum.
	MD5Hash
)

// ParserOp is an operation used when parsing content.
type ParserOp struct {
	Regex,
	XPath string
}

// Parser defines what a parser does.
type Parser struct {
	Name     string
	Type     ParserOutputType `yaml:",omitempty"`
	Priority int              `yaml:",omitempty"`

	Recipie []*ParserOp

	Value   string
	Prepend string `yaml:",omitempty"`
	Append  string `yaml:",omitempty"`

	ExampleURLs []string
}

func (p *Parser) String() string {
	return fmt.Sprintf("ParserDefinition{%d %s %s[%q %q %q]}",
		p.Priority, p.Name, p.Type, p.Prepend, p.Value, p.Append)
}

// ParseHTML applies the parser to HTML.
func (p *Parser) ParseHTML(node *xmlpath.Node) (strpair.Map, error) {
	results := strpair.ParseMap(nil)
	xpath, err := xmlpath.Compile(p.Value)
	if err != nil {
		return nil, err
	}
	if !xpath.Exists(node) {
		return nil, nil
	}
	for iter := xpath.Iter(node); iter.Next(); {
		value := p.Prepend + iter.Node().String() + p.Append
		if value != "" {
			results.Add(p.Type.String(), value)
		}
	}
	return results, nil
}

// ParseJSON applies the parser to JSON.
func (p *Parser) ParseJSON(node *jsonquery.Node) (strpair.Map, error) {
	results := strpair.ParseMap(nil)
	nodes, err := jsonquery.QueryAll(node, p.Value)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		value := p.Prepend + node.InnerText() + p.Append
		results.Add(p.Type.String(), value)
	}
	return results, nil
}

// Parsers contains multiple ParserDefinition instances.
type Parsers []*Parser

// Names provides the names of all included parsers.
func (ps Parsers) Names() []string {
	var names []string
	for _, p := range ps {
		names = append(names, p.Name)
	}
	return names
}

// For returns ParserDefinition instances specified by the URLCLassifier.
func (ps Parsers) For(c *URLClassifier) Parsers {
	var keep Parsers
	parserNames := stringset.NewFromSlice(c.Parsers...)
	for _, p := range ps {
		if parserNames.Has(p.Name) {
			keep = append(keep, p)
		}
	}
	return keep
}

// GetParser returns a parser by name.
func GetParser(name string) *Parser {
	for _, v := range parsers {
		if v.Name == name {
			return v
		}
	}
	return nil
}

// FindParsers returns parsers applicable to the provided URLClassifier.
func FindParsers(uc *URLClassifier) Parsers {
	var ps Parsers
	for _, p := range parsers {
		for _, us := range p.ExampleURLs {
			u, err := url.Parse(us)
			if err != nil {
				logrus.Warn(err)
				continue
			}
			if uc.Match(u) {
				ps = append(ps, p)
			}
		}
	}
	return ps
}
