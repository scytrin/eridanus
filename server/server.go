package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
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
	"golang.org/x/sync/semaphore"
	"gopkg.in/yaml.v2"
)

func runtimeCheck(label string, do func()) {
	start := time.Now()
	do()
	log.Infof("%s: %s", label, time.Now().Sub(start))
}

var serverPersistDelimiter = []byte("---\n")

type Config struct {
	AvoidPersist bool
	StoragePath  string
}

type Server struct {
	Config  `yaml:",omitempty"`
	Cache   `yaml:",omitempty"`
	Similar `yaml:",omitempty"`
	Fetcher `yaml:"-"`

	router http.Handler
}

func (s *Server) Load(path string) error {
	if path == "" {
		return nil
	}

	f, err := os.OpenFile(path, os.O_RDONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := yaml.NewDecoder(f).Decode(s); err != nil {
		return err
	}

	log.Infof("%d entries <= %s", s.Cache.Len(), path)
	log.Infof("%d similar <= %s", s.Similar.Len(), path)

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

	log.Infof("%d entries => %s", s.Cache.Len(), path)
	log.Infof("%d similar => %s", s.Similar.Len(), path)

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

func (s *Server) IngestFile(fileName string, r io.Reader, save bool, tags ...string) (string, error) {
	fileBytes, err := ioutil.ReadAll(r)
	if err != nil {
		return "", err
	}

	idHash := fmt.Sprintf("%x", sha256.Sum256(fileBytes))

	img, imgFormat, err := image.Decode(bytes.NewReader(fileBytes))
	if err != nil {
		return idHash, err
	}

	tags = append(tags,
		fmt.Sprintf("format:%s", imgFormat),
		fmt.Sprintf("filename:%s", fileName),
		fmt.Sprintf("filesize:%d", len(fileBytes)),
		fmt.Sprintf("dimensions:%dx%d", img.Bounds().Size().X, img.Bounds().Size().Y),
	)

	phashTags, err := GeneratePHash(img)
	if err != nil {
		log.Warnf("%s GeneratePHash: %v", idHash, err)
	}

	tags = append(tags, phashTags...)

	if err := s.AddTags(idHash, tags...); err != nil {
		log.Errorf("%s AddTags: %v", idHash, err)
	}

	if save {
		filePath := filepath.Join(s.StoragePath, idHash)
		if err := ioutil.WriteFile(filePath, fileBytes, 0644); err != nil {
			log.Errorf("%s WriteFile: %v", idHash, err)
		}
	}

	return idHash, nil
}

func (s *Server) ImportDir(ctx context.Context, importsDir string, maxWorkers int64, save bool) error {
	dirs, err := filepath.Glob(importsDir)
	if err != nil {
		return err
	}
	w := semaphore.NewWeighted(int64(maxWorkers))
	walkFunc := func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			log.Warn(walkErr)
			return walkErr
		}

		if info.IsDir() {
			return nil
		}

		if err := w.Acquire(ctx, 1); err != nil {
			return err
		}

		go func(path string) {
			defer w.Release(1)
			if err := ctx.Err(); err != nil {
				log.Error(err)
				return
			}

			f, err := os.Open(path)
			if err != nil {
				log.Error(err)
				return
			}
			defer f.Close()

			if _, err := s.IngestFile(filepath.Base(path), f, save); err != nil {
				log.Errorf("%s ingest: %v", path, err)
				return
			}
		}(path)

		return ctx.Err()
	}

	walkStart := time.Now()
	for _, dir := range dirs {
		if err := filepath.Walk(dir, walkFunc); err != nil {
			log.Error(err)
		}
	}
	if err := w.Acquire(ctx, maxWorkers); err != nil {
		log.Errorf("Failed to acquire semaphore: %v", err)
	}
	log.Infof("%s -- walking %s", time.Now().Sub(walkStart), importsDir)

	return nil
}

func (s *Server) findImage(idHash string) (string, error) {
	if s.StoragePath == "" {
		return "", errors.New("no configured storage path")
	}

	if idHash == "" {
		return "", errors.New("blank idHash provided")
	}

	path := filepath.Join(s.StoragePath, "f"+idHash[:2], idHash)

	matches, err := filepath.Glob(path + "*")
	if err != nil {
		return "", err
	}

	if len(matches) < 1 {
		return "", nil
	}

	return matches[0], nil
}
