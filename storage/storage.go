package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // for image processing
	_ "image/jpeg" // for image processing
	_ "image/png"  // for image processing
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/chai2010/webp"
	"github.com/nfnt/resize"
	_ "golang.org/x/image/bmp"      // for image processing
	_ "golang.org/x/image/riff"     // for image processing
	_ "golang.org/x/image/tiff"     // for image processing
	_ "golang.org/x/image/tiff/lzw" // for image processing
	_ "golang.org/x/image/vector"   // for image processing
	_ "golang.org/x/image/vp8"      // for image processing
	_ "golang.org/x/image/vp8l"     // for image processing
	_ "golang.org/x/image/webp"     // for image processing
	"stadik.net/eridanus"
)

type Storage struct {
	Path string
}

func (s *Storage) I() eridanus.Storage {
	return s
}

func (s *Storage) PutContent(ctx context.Context, r io.Reader) (string, error) {
	if s.Path == "" {
		return "", errors.New("Path not set")
	}

	// log := ctxlogrus.Extract(ctx)

	cBytes, err := ioutil.ReadAll(r)
	if err != nil {
		return "", err
	}

	idHash := eridanus.IDHash(cBytes)

	cPath := s.contentPathTransform(idHash)
	if err := os.MkdirAll(filepath.Dir(cPath), 0755); err != nil {
		return "", err
	}
	if err := ioutil.WriteFile(cPath, cBytes, 0644); err != nil {
		return "", err
	}

	return idHash, nil
}

func (s *Storage) GenerateThumbnail(idHash string) error {
	r, err := s.GetContent(idHash)
	if err != nil {
		return err
	}

	img, _, err := image.Decode(r)
	if err != nil {
		return err
	}

	tmb := resize.Thumbnail(180, 180, img, resize.Lanczos3)
	var tBytes []byte
	if err := webp.Encode(bytes.NewBuffer(nil), tmb, nil); err != nil {
		return err
	}

	tPath := s.thumbnailPathTransform(idHash)
	if err := os.MkdirAll(filepath.Dir(tPath), 0755); err != nil {
		return err
	}
	if err := ioutil.WriteFile(tPath, tBytes, 0644); err != nil {
		return err
	}

	return nil
}

func (s *Storage) findFirstFile(basePath string) (string, error) {
	matches, err := filepath.Glob(basePath + "*")
	if err != nil {
		return "", err
	}

	if len(matches) < 1 {
		return "", errors.New("not found")
	}

	return matches[0], nil
}

func (s *Storage) contentPathTransform(idHash string) string {
	return filepath.Join(s.Path, "f"+idHash[:2], idHash)
}

func (s *Storage) GetContentPath(idHash string) (string, error) {
	if idHash == "" {
		return "", errors.New("blank idHash provided")
	}
	path := s.contentPathTransform(idHash)
	match, err := s.findFirstFile(path)
	if err != nil {
		return "", err
	}
	if match == "" {
		return path, nil
	}
	return match, nil
}

func (s *Storage) GetContent(idHash string) (io.Reader, error) {
	path, err := s.GetContentPath(idHash)
	if err != nil {
		return nil, errors.New("no content found")
	}
	return os.Open(path)
}

func (s *Storage) thumbnailPathTransform(idHash string) string {
	return filepath.Join(s.Path, "f"+idHash[:2], idHash)
}

func (s *Storage) GetThumbnailPath(idHash string) (string, error) {
	if idHash == "" {
		return "", errors.New("blank idHash provided")
	}
	path := s.thumbnailPathTransform(idHash)
	match, err := s.findFirstFile(path)
	if err != nil {
		return "", err
	}
	if match == "" {
		return path, nil
	}
	return match, nil
}

func (s *Storage) GetThumbnail(idHash string) (io.Reader, error) {
	path, err := s.GetThumbnailPath(idHash)
	if err != nil {
		return nil, errors.New("no content found")
	}
	return os.Open(path)
}

type writeCount int

func (c *writeCount) Write(p []byte) (n int, err error) {
	*c = writeCount(int(*c) + len(p))
	return len(p), nil
}

func (s *Storage) FileInfoTags(ctx context.Context, idHash string) ([]string, error) {
	// log := ctxlogrus.Extract(ctx)

	r, err := s.GetContent(idHash)
	if err != nil {
		return nil, err
	}

	var c writeCount
	img, imgFormat, err := image.Decode(io.TeeReader(r, &c))
	if err != nil {
		return nil, err
	}

	// tags, err := similar.GeneratePHash(img)
	// if err != nil {
	// 	log.Warnf("%s GeneratePHash: %v", idHash, err)
	// }

	tags := []string{
		fmt.Sprintf("format:%s", imgFormat),
		fmt.Sprintf("filesize:%d", c),
		fmt.Sprintf("dimensions:%dx%d", img.Bounds().Size().X, img.Bounds().Size().Y),
	}

	return tags, nil
}
