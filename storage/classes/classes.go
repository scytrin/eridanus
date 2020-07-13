package classes

import (
	"bytes"
	"net/url"
	"os"

	"github.com/pkg/errors"
	"github.com/scytrin/eridanus"
	"gopkg.in/yaml.v3"
)

const (
	classesBlobKey = "config/classes.yaml"
)

type classStorage struct {
	be      eridanus.StorageBackend
	classes []*eridanus.URLClass
}

// NewClassesStorage provides a new ClassesStorage.
func NewClassesStorage(be eridanus.StorageBackend) eridanus.ClassesStorage {
	return &classStorage{be, nil}
}

func (s *classStorage) load() error {
	rc, err := s.be.Get(classesBlobKey)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	defer rc.Close()
	if err := yaml.NewDecoder(rc).Decode(&s.classes); err != nil {
		return err
	}
	if err != nil || len(s.classes) == 0 {
		s.classes = eridanus.DefaultClasses() // only if none existing
	}
	return nil
}

func (s *classStorage) save() error {
	b, err := yaml.Marshal(s.classes)
	if err != nil {
		return err
	}
	return s.be.Set(classesBlobKey, bytes.NewReader(b))
}

// GetAll returns all current classifiers.
func (s *classStorage) GetAll() ([]*eridanus.URLClass, error) {
	var classes []*eridanus.URLClass
	rc, err := s.be.Get(classesBlobKey)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		return eridanus.DefaultClasses(), err
	}
	defer rc.Close()
	if err := yaml.NewDecoder(rc).Decode(&classes); err != nil {
		return nil, err
	}
	if err != nil || len(classes) == 0 {
		classes = eridanus.DefaultClasses() // only if none existing
	}
	return classes, nil
}

// Add adds a classifier.
func (s *classStorage) Add(c *eridanus.URLClass) error {
	classes, err := s.GetAll()
	if err != nil {
		return err
	}
	classes = append(classes, c)
	pBytes, err := yaml.Marshal(classes)
	if err != nil {
		return err
	}
	return s.be.Set(classesBlobKey, bytes.NewReader(pBytes))
}

// GetByName returns the named classifier.
func (s *classStorage) GetByName(name string) (*eridanus.URLClass, error) {
	classes, err := s.GetAll()
	if err != nil {
		return nil, err
	}
	for _, c := range classes {
		if c.Name == name {
			return c, nil
		}
	}
	return nil, errors.Errorf("no classifier named %s", name)
}

func (s *classStorage) For(u *url.URL) (*eridanus.URLClass, error) {
	var ucKeep *eridanus.URLClass
	var urlKeep *url.URL

	classes, err := s.GetAll()
	if err != nil {
		return nil, err
	}

	for _, uc := range classes {
		if ucKeep != nil && ucKeep.GetPriority() >= uc.GetPriority() {
			continue
		}
		nu, err := eridanus.ApplyClassifier(uc, u)
		if err != nil {
			continue
		}
		ucKeep, urlKeep = uc, nu
	}
	if ucKeep != nil && urlKeep != nil {
		return ucKeep, nil
	}
	return nil, errors.Errorf("no classifier for %s", u)
}
