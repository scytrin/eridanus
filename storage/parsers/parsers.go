package parsers

import (
	"bytes"
	"fmt"
	"net/url"

	"github.com/scytrin/eridanus"
	"gopkg.in/yaml.v3"
)

const (
	parsersNamespace = "parsers"
	parsersBlobKey   = "config/parsers.yaml"
)

type parsersStorage struct{ be eridanus.StorageBackend }

// NewParsersStorage provides a new ParsersStorage.
func NewParsersStorage(be eridanus.StorageBackend) eridanus.ParsersStorage {
	return &parsersStorage{be}
}

// Keys returns a list of all parser names.
func (s *parsersStorage) Keys() ([]string, error) {
	return s.be.Keys(parsersNamespace)
}

// AddParser adds a parser.
func (s *parsersStorage) Add(p *eridanus.Parser) error {
	pPath := fmt.Sprintf("%s/%s", parsersNamespace, p.GetName())
	buf := bytes.NewBuffer(nil)
	if err := yaml.NewEncoder(buf).Encode(p); err != nil {
		return err
	}
	return s.be.Set(pPath, buf)
}

// Get returns the named parser.
func (s *parsersStorage) Get(name string) (*eridanus.Parser, error) {
	pPath := fmt.Sprintf("%s/%s", parsersNamespace, name)
	rc, err := s.be.Get(pPath)
	if err != nil {
		return nil, err
	}
	var retval eridanus.Parser
	if err := yaml.NewDecoder(rc).Decode(&retval); err != nil {
		return nil, err
	}
	return &retval, nil
}

// GetAllParsers returns all current parsers.
func (s *parsersStorage) GetAll() ([]*eridanus.Parser, error) {
	var vs []*eridanus.Parser
	keys, err := s.be.Keys(parsersNamespace)
	for _, k := range keys {
		rc, err := s.be.Get(k)
		if err != nil {
			return nil, err
		}
		defer rc.Close()

		var v *eridanus.Parser
		if err := yaml.NewDecoder(rc).Decode(&v); err != nil {
			return nil, err
		}
		vs = append(vs, v)
	}
	if err != nil || len(vs) == 0 {
		vs = eridanus.DefaultParsers() // only if none existing
	}
	return vs, nil
}

// For returns a list of parsers applicable to the provided URLClass.
func (s *parsersStorage) For(c *eridanus.URLClass) ([]*eridanus.Parser, error) {
	parsers, err := s.GetAll()
	if err != nil {
		return nil, err
	}

	var keep []*eridanus.Parser
	pts := eridanus.ClassifierParserTypes[c.GetClass()]
	for _, p := range parsers {
		var has bool
		for _, pt := range pts {
			has = has || p.GetType() == pt
		}
		if !has {
			continue
		}
		for _, ru := range p.GetUrls() {
			u, err := url.Parse(ru)
			if err != nil {
				return nil, err
			}
			if _, err := eridanus.ApplyClassifier(c, u); err == nil {
				keep = append(keep, p)
				break
			}
		}
	}
	return keep, nil
}
