package parsers

import (
	"bytes"
	"net/url"
	"os"

	"github.com/pkg/errors"
	"github.com/scytrin/eridanus"
	"gopkg.in/yaml.v3"
)

const (
	parsersBlobKey = "config/parsers.yaml"
)

type parsersStorage struct {
	be eridanus.StorageBackend
}

// NewParsersStorage provides a new ParsersStorage.
func NewParsersStorage(be eridanus.StorageBackend) eridanus.ParsersStorage {
	return &parsersStorage{be}
}

// GetAllParsers returns all current parsers.
func (s *parsersStorage) GetAll() ([]*eridanus.Parser, error) {
	var parsers []*eridanus.Parser
	rc, err := s.be.Get(parsersBlobKey)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		return eridanus.DefaultParsers(), nil
	}
	defer rc.Close()
	if err := yaml.NewDecoder(rc).Decode(&parsers); err != nil {
		return nil, err
	}
	if err != nil || len(parsers) == 0 {
		return eridanus.DefaultParsers(), err // only if none existing
	}
	return parsers, nil
}

// AddParser adds a parser.
func (s *parsersStorage) Add(p *eridanus.Parser) error {
	parsers, err := s.GetAll()
	if err != nil {
		return err
	}
	parsers = append(parsers, p)
	pBytes, err := yaml.Marshal(parsers)
	if err != nil {
		return err
	}
	return s.be.Set(parsersBlobKey, bytes.NewReader(pBytes))
}

// GetByName returns the named parser.
func (s *parsersStorage) GetByName(name string) (*eridanus.Parser, error) {
	parsers, err := s.GetAll()
	if err != nil {
		return nil, err
	}
	for _, p := range parsers {
		if p.Name == name {
			return p, nil
		}
	}
	return nil, errors.Errorf("no parser named %s", name)
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
