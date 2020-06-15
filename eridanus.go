// Package eridanus is an implementation of a content retrieval, storage, and
// categorizational system inspired by Hydrus Network.
package eridanus

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/improbable-eng/go-httpwares/logging/logrus/ctxlogrus"
	"github.com/sirupsen/logrus"
	"gocloud.dev/blob"
	"golang.org/x/xerrors"
	"gopkg.in/yaml.v2"
)

var (
	// ErrItemNotFound is an identifiable error for "NotFound"
	ErrItemNotFound = xerrors.New("item not found")
)

// Storage manages content.
type Storage interface {
	http.CookieJar

	// AddParser(context.Context, *Parser) error
	// GetParser(context.Context, string) (*Parser, error)
	// FindParsers(context.Context, *URLClassifier) ([]*Parser, error)

	// AddClassifier(context.Context, *URLClassifier) error
	// GetClassifier(context.Context, string) (*URLClassifier, error)
	// FindClassifier(context.Context, *url.URL) (*URLClassifier, error)

	Close() error

	PutData(ctx context.Context, path string, r io.Reader, opts *blob.WriterOptions) error
	GetData(ctx context.Context, path string, opts *blob.ReaderOptions) (io.Reader, error)

	PutTags(idHash string, tags []string) error
	GetTags(idHash string) ([]string, error)
	Find() ([]string, error)

	ContentKeys() ([]string, error)
	PutContent(ctx context.Context, r io.Reader) (idHash string, err error)
	GetContent(idHash string) (io.Reader, error)

	GetThumbnail(idHash string) (io.Reader, error)
}

const (
	contentNamespace = "content"
	classesBlobKey   = "classes.yaml"
	parsersBlobKey   = "parsers.yaml"
)

// RecoveryHandler allows for handling panics.
func RecoveryHandler(f func(error)) {
	r := recover()
	if r == nil {
		return
	}
	logrus.Debug(r)

	var err error
	switch rerr := r.(type) {
	case error:
		err = rerr
	case string:
		err = xerrors.New(rerr)
	default:
		err = xerrors.Errorf("panicked: %v", rerr)
	}
	f(err)
}

// Eridanus is an implementation of a content retrieval, storage, and
// categorizational system inspired by Hydrus Network.
type Eridanus struct {
	storage Storage
	parsers []*Parser
	classes []*URLClassifier
}

// New returns a new instance.
func New(ctx context.Context, s Storage) (*Eridanus, error) {
	e := &Eridanus{storage: s}

	if err := e.loadClassesFromStorage(ctx); err != nil {
		return nil, err
	}

	if err := e.loadParsersFromStorage(ctx); err != nil {
		return nil, err
	}

	ctxlogrus.Extract(ctx).Infof("%#v", e)

	return e, nil
}

func (e *Eridanus) loadParsersFromStorage(ctx context.Context) error {
	r, err := e.storage.GetData(ctx, parsersBlobKey, nil)
	if err != nil {
		if err != ErrItemNotFound {
			return err
		}
		e.parsers = defaultConfig.GetParsers()
		return nil
	}
	return yaml.NewDecoder(r).Decode(&e.parsers)
}

func (e *Eridanus) loadClassesFromStorage(ctx context.Context) error {
	r, err := e.storage.GetData(ctx, classesBlobKey, nil)
	if err != nil {
		if err != ErrItemNotFound {
			return err
		}
		e.classes = defaultConfig.GetClasses()
		return nil
	}
	return yaml.NewDecoder(r).Decode(&e.classes)
}

// Close closes open instances.
func (e *Eridanus) Close() error {
	ctx := context.Background()

	if err := e.saveClassesToStorage(ctx); err != nil {
		return err
	}

	if err := e.saveParsersToStorage(ctx); err != nil {
		return err
	}

	if err := e.storage.Close(); err != nil {
		return err
	}

	return nil
}

func (e *Eridanus) saveParsersToStorage(ctx context.Context) error {
	b, err := yaml.Marshal(e.parsers)
	if err != nil {
		return err
	}
	return e.storage.PutData(ctx, parsersBlobKey, bytes.NewReader(b), nil)
}

func (e *Eridanus) saveClassesToStorage(ctx context.Context) error {
	b, err := yaml.Marshal(e.classes)
	if err != nil {
		return err
	}
	return e.storage.PutData(ctx, classesBlobKey, bytes.NewReader(b), nil)
}

// GetStorage returns the current storage instance.
func (e *Eridanus) GetStorage() Storage {
	return e.storage
}

// GetClassifiers returns all current classifiers.
func (e *Eridanus) GetClassifiers() []*URLClassifier {
	return e.classes
}

// GetParsers returns all current parsers.
func (e *Eridanus) GetParsers() []*Parser {
	return e.parsers
}
