package classes

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	"github.com/pkg/errors"
	"github.com/scytrin/eridanus"
	"gopkg.in/yaml.v3"
)

const (
	classesNamespace = "classes"
)

type classStorage struct{ be eridanus.StorageBackend }

// NewClassesStorage provides a new ClassesStorage.
func NewClassesStorage(be eridanus.StorageBackend) eridanus.ClassesStorage {
	return &classStorage{be}
}

// Names returns a list of all class names.
func (s *classStorage) Names() ([]string, error) {
	keys, err := s.be.Keys(classesNamespace)
	if err != nil {
		return nil, err
	}
	for i, k := range keys {
		keys[i] = strings.TrimPrefix(k, classesNamespace+"/")
	}
	return keys, nil
}

// Put adds a classifier.
func (s *classStorage) Put(c *eridanus.URLClass) error {
	cPath := fmt.Sprintf("%s/%s", classesNamespace, c.GetName())
	buf := bytes.NewBuffer(nil)
	if err := yaml.NewEncoder(buf).Encode(c); err != nil {
		return err
	}
	return s.be.Set(cPath, buf)
}

func (s *classStorage) Has(name string) bool {
	cPath := fmt.Sprintf("%s/%s", classesNamespace, name)
	return s.be.Has(cPath)
}

// Get returns the named classifier.
func (s *classStorage) Get(name string) (*eridanus.URLClass, error) {
	cPath := fmt.Sprintf("%s/%s", classesNamespace, name)
	rc, err := s.be.Get(cPath)
	if err != nil {
		return nil, err
	}
	var retval eridanus.URLClass
	if err := yaml.NewDecoder(rc).Decode(&retval); err != nil {
		return nil, err
	}
	return &retval, nil
}

// GetAll returns all current classifiers.
func (s *classStorage) GetAll() ([]*eridanus.URLClass, error) {
	var vs []*eridanus.URLClass
	keys, err := s.be.Keys(classesNamespace)
	for _, k := range keys {
		rc, err := s.be.Get(k)
		if err != nil {
			return nil, err
		}
		defer rc.Close()

		var v *eridanus.URLClass
		if err := yaml.NewDecoder(rc).Decode(&v); err != nil {
			return nil, err
		}
		vs = append(vs, v)
	}
	if err != nil || len(vs) == 0 {
		vs = eridanus.DefaultClasses() // only if none existing
	}
	return vs, nil
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
