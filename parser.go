package eridanus

import (
	"fmt"
	"strings"

	"github.com/antchfx/jsonquery"
	"go.chromium.org/luci/common/data/stringset"
	"go.chromium.org/luci/common/data/strpair"
	"gopkg.in/xmlpath.v2"
)

//go:generate stringer -type=ParserOutputType
type ParserOutputType int

func (t ParserOutputType) MarshalYAML() interface{} {
	return t.String()
}

func (t *ParserOutputType) UnmarshalYAML(unm func(interface{}) error) error {
	var i interface{}
	if err := unm(&i); err != nil {
		return err
	}
	switch v := i.(type) {
	case ParserOutputType:
		*t = v
	case int:
		*t = ParserOutputType(v)
	case string:
		for _, e := range ParserOutputTypes {
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

// Array of ParserOutputType enum values.
var ParserOutputTypes = [...]ParserOutputType{
	Content,
	Tag,
	Follow,
	Title,
	Source,
	MD5Hash,
}

// https://devhints.io/xpath
type ParserDefinition struct {
	Name     string
	Type     ParserOutputType `yaml:",omitempty"`
	Priority int              `yaml:",omitempty"`

	Value   string
	Prepend string `yaml:",omitempty"`
	Append  string `yaml:",omitempty"`
}

func (p *ParserDefinition) String() string {
	return fmt.Sprintf("ParserDefinition{%d %s %s[%q %q %q]}",
		p.Priority, p.Name, p.Type, p.Prepend, p.Value, p.Append)
}

func (p *ParserDefinition) ParseHTML(node *xmlpath.Node) (strpair.Map, error) {
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
		results.Add(p.Type.String(), value)
	}
	return results, nil
}

func (p *ParserDefinition) ParseJSON(node *jsonquery.Node) (strpair.Map, error) {
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

type ParserDefinitions []*ParserDefinition

func (ps ParserDefinitions) For(c *URLClassifier) ParserDefinitions {
	var keep ParserDefinitions
	parserNames := stringset.NewFromSlice(c.Parsers...)
	for _, p := range ps {
		if parserNames.Has(p.Name) {
			keep = append(keep, p)
		}
	}
	return keep
}