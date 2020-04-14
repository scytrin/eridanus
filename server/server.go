package server

import (
	"context"
	"crypto/sha1"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math/big"
	"os"

	"github.com/azr/phash"
	"github.com/corona10/goimagehash"
	"github.com/dgryski/go-simstore"
	log "github.com/sirupsen/logrus"
)

const (
	simstoreSize = 1e6
)

var (
	simstoreFactory = simstore.NewZStore // simstore.NewU64Slice
)

type Config struct {
	StoragePath string
}

type Server struct {
	Cache
	cfg    Config
	ctx    context.Context
	cancel func()

	simstore simstore.Storage
}

func NewServer(cfg Config) (*Server, error) {
	if err := os.MkdirAll(cfg.StoragePath, 0755); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{cfg: cfg, ctx: ctx, cancel: cancel}
	log.Info(s)
	return s, nil
}

func (s *Server) FindSimilar() (map[string][]string, error) {
	if s.simstore == nil {
		if err := s.BuildPhashStore(); err != nil {
			return nil, err
		}
	}

	wantTag := "phash:phash:"
	wantTagLen := len(wantTag)
	items := make(map[string][]string)
	if err := s.Range(func(item string, tags []string) error {
		var itemHash, percHash big.Int
		if _, ok := itemHash.SetString(item, 16); !ok {
			return fmt.Errorf("unable to parse ihash %s", item)
		}
		for _, tag := range tags {
			if len(tag) <= wantTagLen || tag[:wantTagLen] != wantTag {
				continue
			}
			if _, ok := percHash.SetString(tag[wantTagLen:], 16); !ok {
				return fmt.Errorf("unable to parse %s", tag[wantTagLen:])
			}

			if percHash.Uint64() == 0 {
				return nil
			}

			similarInts := s.simstore.Find(percHash.Uint64())
			if len(similarInts) < 1 {
				return nil
			}

			similar := make([]string, len(similarInts))
			for i, similarInt := range similarInts {
				var similarHash big.Int
				similarHash.SetUint64(similarInt)
				similar[i] = similarHash.Text(16)
			}
			log.Print(similar)

			items[item] = similar
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return items, nil
}

func (s *Server) BuildPhashStore() error {
	//simstore := simstore.New3Small(simstoreSize)
	//simstore := simstore.New3(simstoreSize, simstoreFactory)
	simstore := simstore.New6(simstoreSize, simstoreFactory)
	wantTag := "phash:phash:"
	wantTagLen := len(wantTag)
	if err := s.Range(func(item string, tags []string) error {
		var itemHash, percHash big.Int
		if _, ok := itemHash.SetString(item, 16); !ok {
			return fmt.Errorf("unable to parse ihash %s", item)
		}
		for _, tag := range tags {
			if len(tag) <= wantTagLen || tag[:wantTagLen] != wantTag {
				continue
			}
			if _, ok := percHash.SetString(tag[wantTagLen:], 16); !ok {
				return fmt.Errorf("unable to parse %s", tag[wantTagLen:])
			}
			simstore.Add(percHash.Uint64(), itemHash.Uint64())
		}
		return nil
	}); err != nil {
		return err
	}
	simstore.Finish()
	s.simstore = simstore

	return nil
}

func (s *Server) buildPHash(data io.Reader) (uint64, error) {
	img, _, err := image.Decode(data)
	if err != nil {
		return 0, err
	}
	log.Print(phash.DTC(img))
	log.Print(goimagehash.PerceptionHash(img))
	return phash.DTC(img), nil
}

func (s *Server) buildHash(data io.Reader) (string, error) {
	var itemHash big.Int
	fileHash := sha1.New()
	io.Copy(fileHash, data)
	itemHash.SetBytes(fileHash.Sum(nil))
	return itemHash.Text(16), nil
}
