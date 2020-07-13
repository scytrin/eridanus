package backend

import (
	"io"
	"path/filepath"
	"strings"

	"github.com/peterbourgon/diskv/v3"
	"github.com/sirupsen/logrus"
)

// var DefaultOpts = diskv.Options{
// 	BasePath:     filepath.Join(s.rootPath, "kv"),
// 	CacheSizeMax: 8e+7,
// 	PathPerm:     0755,
// 	FilePerm:     0644,
// 	AdvancedTransform: func(s string) *diskv.PathKey {
// 		path := strings.Split(s, string(filepath.Separator))
// 		last := len(path) - 1
// 		return &diskv.PathKey{Path: path[:last], FileName: path[last]}
// 	},
// 	InverseTransform: func(pk *diskv.PathKey) string {
// 		return filepath.Join(append(pk.Path, pk.FileName)...)
// 	},
// }

// DiskvBackend provides a diskv storage backend.
type DiskvBackend struct {
	kv *diskv.Diskv
}

// NewDiskvBackend provides a new backend instance.
func NewDiskvBackend(rootPath string) *DiskvBackend {
	be := &DiskvBackend{}

	be.kv = diskv.New(diskv.Options{
		BasePath:          filepath.Join(rootPath, "kv"),
		InverseTransform:  be.pathKeyToKey,
		AdvancedTransform: be.keyToPathKey,
		// CacheSizeMax:      8e+7,
		Compression: nil,
	})

	return be
}

func (be *DiskvBackend) keyToPathKey(key string) *diskv.PathKey {
	logrus.Debug(key)
	key = filepath.Clean(filepath.FromSlash(key))
	path := strings.Split(key, string(filepath.Separator))
	last := len(path) - 1
	return &diskv.PathKey{Path: path[:last], FileName: path[last]}
}

func (be *DiskvBackend) pathKeyToKey(pk *diskv.PathKey) string {
	return filepath.ToSlash(filepath.Join(append(pk.Path, pk.FileName)...))
}

// GetRootPath returns the filepath location of on disk storage.
func (be *DiskvBackend) GetRootPath() string {
	return be.kv.BasePath
}

// Close closes any open items.
func (be *DiskvBackend) Close() error {
	return nil
}

// Import ingests a file on the local filesystem and stores it at the specified key.
//
// If move is true, the file will be removed after import.
func (be *DiskvBackend) Import(srcPath, key string, move bool) error {
	return be.kv.Import(srcPath, key, move)
}

// Has checks for the presence of arbitrary data.
func (be *DiskvBackend) Has(key string) bool {
	return be.kv.Has(key)
}

// Set stores arbitrary data.
func (be *DiskvBackend) Set(key string, r io.Reader) error {
	return be.kv.WriteStream(key, r, false)
}

// Get fetches arbitrary data.
func (be *DiskvBackend) Get(key string) (io.ReadCloser, error) {
	return be.kv.ReadStream(key, false)
}

// Delete removes arbitrary data.
func (be *DiskvBackend) Delete(key string) error {
	return be.kv.Erase(key)
}

// Keys returns a list of all keys under the provided prefix.
func (be *DiskvBackend) Keys(prefix string) ([]string, error) {
	var keys []string
	for key := range be.kv.KeysPrefix(prefix, nil) {
		keys = append(keys, key)
	}
	return keys, nil
}
