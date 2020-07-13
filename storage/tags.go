package storage

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/scytrin/eridanus"
)

type tagStorage struct{ eridanus.StorageBackend }

// NewTagStorage provides a new TagStorage.
func NewTagStorage(be eridanus.StorageBackend) eridanus.TagStorage {
	return &tagStorage{be}
}

// TagKeys returns a list of all tag item keys.
func (s *tagStorage) TagKeys() (eridanus.IDHashes, error) {
	var idHashes eridanus.IDHashes
	keys, err := s.Keys(metadataNamespace)
	if err != nil {
		return nil, err
	}
	for _, k := range keys {
		idHashes = append(idHashes, eridanus.IDHash(k))
	}
	return idHashes, nil
}

// GetTags provides a string slice of tags for the given hash.
func (s *tagStorage) GetTags(idHash eridanus.IDHash) (eridanus.Tags, error) {
	mPath := fmt.Sprintf("%s/%s", metadataNamespace, idHash)
	r, err := s.GetData(mPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	tagStrs := strings.Split(string(b), ",")
	var tags eridanus.Tags
	for _, t := range tagStrs {
		tags = append(tags, eridanus.Tag(t))
	}
	return tags.OmitDuplicates(), nil
}

// HasTags indicates if tags exist for the given hash.
func (s *tagStorage) HasTags(idHash eridanus.IDHash) bool {
	mPath := fmt.Sprintf("%s/%s", metadataNamespace, idHash)
	return s.StorageBackend.HasData(mPath)
}

// SetTags sets a string slice of tags for the given hash.
func (s *tagStorage) SetTags(idHash eridanus.IDHash, newTags eridanus.Tags) error {
	mPath := fmt.Sprintf("%s/%s", metadataNamespace, idHash)
	tagStr := newTags.OmitDuplicates().String()
	return s.SetData(mPath, strings.NewReader(tagStr))
}

// Find searches through tags for matches.
func (s *tagStorage) FindByTags() (eridanus.IDHashes, error) {
	var idHashes eridanus.IDHashes
	keys, err := s.Keys(metadataNamespace)
	if err != nil {
		return nil, err
	}
	for _, mPath := range keys {
		idHash := eridanus.IDHash(filepath.Base(mPath))
		tags, err := s.GetTags(idHash)
		if err != nil {
			return nil, err
		}
		if len(tags) > 0 {
			idHashes = append(idHashes, idHash)
		}
	}
	return idHashes, nil
}
