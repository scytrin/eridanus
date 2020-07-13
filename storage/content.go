package storage

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/ioutil"

	"github.com/nfnt/resize"
	"github.com/scytrin/eridanus"
	"github.com/scytrin/eridanus/idhash"
)

type contentStorage struct{ eridanus.StorageBackend }

// NewContentStorage provides a new ContentStorage.
func NewContentStorage(be eridanus.StorageBackend) eridanus.ContentStorage {
	return &contentStorage{be}
}

// ContentKeys returns a list of all content item keys.
func (s *contentStorage) ContentKeys() (eridanus.IDHashes, error) {
	var idHashes eridanus.IDHashes
	keys, err := s.Keys(contentNamespace)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		idHashes = append(idHashes, eridanus.IDHash(k))
	}
	return idHashes, nil
}

// HasContent checks of the presence of content for the given hash.
func (s *contentStorage) HasContent(idHash eridanus.IDHash) bool {
	cPath := fmt.Sprintf("%s/%s", contentNamespace, idHash)
	return s.HasData(cPath)
}

// SetContent adds content, returning the hash.
func (s *contentStorage) SetContent(r io.Reader) (out eridanus.IDHash, err error) {
	cBytes, err := ioutil.ReadAll(r)
	if err != nil {
		return "", err
	}

	idHash, err := idhash.GenerateIDHash(bytes.NewReader(cBytes))
	if err != nil {
		return "", err
	}

	cPath := fmt.Sprintf("%s/%s", contentNamespace, idHash)
	if err := s.SetData(cPath, bytes.NewReader(cBytes)); err != nil {
		return "", err
	}

	return idHash, nil
}

func (s *contentStorage) generateThumbnail(idHash eridanus.IDHash) (err error) {
	defer eridanus.RecoveryHandler(func(e error) { err = e })

	r, err := s.GetContent(idHash)
	if err != nil {
		return err
	}

	img, _, err := image.Decode(r)
	if err != nil {
		return err
	}

	tBuf := bytes.NewBuffer(nil)
	tImg := resize.Thumbnail(150, 150, img, resize.NearestNeighbor)
	if err := png.Encode(tBuf, tImg); err != nil {
		return err
	}

	tPath := fmt.Sprintf("%s/%s", thumbnailNamespace, idHash)
	return s.SetData(tPath, tBuf)
}

// GetContent provides a reader of the content for the given hash.
func (s *contentStorage) GetContent(idHash eridanus.IDHash) (io.ReadCloser, error) {
	cPath := fmt.Sprintf("%s/%s", contentNamespace, idHash)
	return s.GetData(cPath)
}

// GetThumbnail provides a reader of the thumbnail for the given hash.
func (s *contentStorage) GetThumbnail(idHash eridanus.IDHash) (io.ReadCloser, error) {
	tPath := fmt.Sprintf("%s/%s", thumbnailNamespace, idHash)
	if !s.HasData(tPath) {
		if err := s.generateThumbnail(idHash); err != nil {
			return nil, err
		}
	}
	return s.GetData(tPath)
}
