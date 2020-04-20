package server

import (
	"context"
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/riff"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/tiff/lzw"
	_ "golang.org/x/image/vector"
	_ "golang.org/x/image/vp8"
	_ "golang.org/x/image/vp8l"
	_ "golang.org/x/image/webp"
	"gopkg.in/yaml.v2"
)

func runtimeCheck(label string, do func()) {
	start := time.Now()
	do()
	log.Infof("%s: %s", label, time.Now().Sub(start))
}

type Server struct {
	Port int
	AvoidPersist,
	SaveImports,
	SaveUploads bool

	Storage `yaml:",omitempty"`
	Fetcher `yaml:"-"`
	Cache   `yaml:",omitempty"`
	Similar `yaml:",omitempty"`

	cfgRouter sync.Once
	router    http.Handler
}

func (s *Server) Load(path string) error {
	if path == "" {
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := yaml.NewDecoder(f).Decode(s); err != nil {
		return err
	}

	log.Infof("%s => %d entries, %d similar",
		path, s.Cache.Len(), s.Similar.Len())

	return nil
}

func (s *Server) Save(path string) error {
	if s.AvoidPersist || path == "" {
		return nil
	}

	at := fmt.Sprint(time.Now().Unix())

	newPath := path + "." + at + ".new"
	f, err := os.OpenFile(newPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	log.Infof("%s <= %d entries, %d similar",
		path, s.Cache.Len(), s.Similar.Len())

	if err := yaml.NewEncoder(f).Encode(s); err != nil {
		return err
	}
	f.Close()

	oldPath := path + "." + at + ".bak"
	if err := os.Rename(path, oldPath); err != nil {
		log.Warn(err)
	}

	return os.Rename(newPath, path)
}

func (s *Server) ImportDir(ctx context.Context, importsDir string, maxWorkers int) error {
	walkStart := time.Now()
	defer log.Infof("%s -- walking %s",
		time.Now().Sub(walkStart), importsDir)

	dirs, err := filepath.Glob(importsDir)
	if err != nil {
		return err
	}

	pathChan := make(chan string, 1)
	go func() {
		defer close(pathChan)
		for _, dir := range dirs {
			if err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
				if walkErr != nil {
					log.Warn(walkErr)
					return walkErr
				}
				if !info.IsDir() {
					pathChan <- path
				}
				return nil
			}); err != nil {
				log.Error(err)
			}
		}
	}()

	importWorker := func(wg *sync.WaitGroup, path string) {
		defer wg.Done()

		f, err := os.Open(path)
		if err != nil {
			log.Error(err)
		}
		defer f.Close()

		idHash, tags, err := s.Storage.Ingest(f, false)
		if err != nil {
			log.Error(err)
			return
		}

		tags = append(tags,
			fmt.Sprintf("filename:%s", filepath.Base(path)))
		if err := s.AddTags(idHash, tags...); err != nil {
			log.Errorf("ingest: %v", err)
		}
	}

	var wg sync.WaitGroup
	for path := range pathChan {
		wg.Add(1)
		go importWorker(&wg, path)
	}
	wg.Wait()

	return nil
}
