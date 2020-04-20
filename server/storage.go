package server

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"io"
	"io/ioutil"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

type Storage struct {
	Path string
}

func (s *Storage) Ingest(r io.Reader, noSave bool) (string, []string, error) {
	fileBytes, err := ioutil.ReadAll(r)
	if err != nil {
		return "", nil, err
	}

	idHash := fmt.Sprintf("%x", sha256.Sum256(fileBytes))

	img, imgFormat, err := image.Decode(bytes.NewReader(fileBytes))
	if err != nil {
		return "", nil, err
	}

	tags, err := GeneratePHash(img)
	if err != nil {
		log.Warnf("%s GeneratePHash: %v", idHash, err)
	}

	tags = append(tags,
		fmt.Sprintf("format:%s", imgFormat),
		fmt.Sprintf("filesize:%d", len(fileBytes)),
		fmt.Sprintf("dimensions:%dx%d", img.Bounds().Size().X, img.Bounds().Size().Y))

	if !noSave {
		if s.Path == "" {
			log.Warnf("Path not set, not saving %s", idHash)
		} else {
			filePath := s.ImagePath(idHash)
			if err := ioutil.WriteFile(filePath, fileBytes, 0644); err != nil {
				return "", nil, fmt.Errorf("%s copy: %v", filePath, err)
			}
		}
	}

	return idHash, tags, nil
}

func (s *Storage) ImagePath(idHash string) string {
	return filepath.Join(s.Path, "f"+idHash[:2], idHash)
}

func (s *Storage) FindImage(idHash string) (string, error) {
	if idHash == "" {
		return "", errors.New("blank idHash provided")
	}

	matches, err := filepath.Glob(s.ImagePath(idHash) + "*")
	if err != nil {
		return "", err
	}

	if len(matches) < 1 {
		return "", nil
	}

	return matches[0], nil
}

func (s *Storage) ThumbnailPath(idHash string) string {
	return filepath.Join(s.Path, "t"+idHash[:2], idHash)
}

func (s *Storage) FindThumbnail(idHash string) (string, error) {
	if idHash == "" {
		return "", errors.New("blank idHash provided")
	}

	matches, err := filepath.Glob(s.ThumbnailPath(idHash) + "*")
	if err != nil {
		return "", err
	}

	if len(matches) < 1 {
		return "", nil
	}

	return matches[0], nil
}
