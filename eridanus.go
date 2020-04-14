package eridanus

import (
  "bytes"
  "context"
  "crypto/sha1"
  "fmt"
  "image"
  _ "image/png"
  _ "image/gif"
  _ "image/jpeg"
  "math/big"

  log "github.com/sirupsen/logrus"
	"github.com/dgryski/go-simstore"
  "github.com/azr/phash"
  "github.com/corona10/goimagehash"
)

const (
	simstoreSize = 1e8
)

var (
	simstoreFactory = simstore.NewZStore // simstore.NewU64Slice
)

type Config struct {
  StoragePath string
}

type Server struct {
  Cache
  cfg Config
	ctx    context.Context
  cancel func()
  
	simstore simstore.Storage
}

func NewServer(cfg Config) (*Server, error) {
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

  items := make(map[string][]string)
  if err := s.Range(func(item string, tags []string) error {
    var itemHash, percHash big.Int
    if _, ok := itemHash.SetString(item, 16); !ok {
      return fmt.Errorf("unable to parse ihash %s", item)
    }
    for _, tag := range tags {
      if tag[:6] != "phash:" {
        continue
      }
      if _, ok := percHash.SetString(tag[6:], 16); !ok {
        return fmt.Errorf("unable to parse phash %s", tag[6:])
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
  if err := s.Range(func(item string, tags []string) error {
    var itemHash, percHash big.Int
    if _, ok := itemHash.SetString(item, 16); !ok {
      return fmt.Errorf("unable to parse ihash %s", item)
    }
    for _, tag := range tags {
      if tag[:6] != "phash:" {
        continue
      }
      if _, ok := percHash.SetString(tag[6:], 16); !ok {
        return fmt.Errorf("unable to parse phash %s", tag[6:])
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

func (s *Server) buildPHash(fileBytes []byte) (uint64, error) {
  img, _, err := image.Decode(bytes.NewReader(fileBytes))
  if err != nil {
    return 0, err
  }
  log.Print(phash.DTC(img))
  log.Print(goimagehash.PerceptionHash(img))
  return phash.DTC(img), nil
}

func (s *Server) buildHash(fileBytes []byte) (string, error) {
  var itemHash big.Int
  hashSum := sha1.Sum(fileBytes)
  itemHash.SetBytes(hashSum[:])
  return itemHash.Text(16), nil
}
