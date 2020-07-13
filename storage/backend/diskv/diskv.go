package diskv

import (
	"io"
	"path/filepath"
	"strings"

	"github.com/peterbourgon/diskv/v3"
	"github.com/sirupsen/logrus"
)

func keyToPathKey(key string) *diskv.PathKey {
	logrus.Debug(key)
	key = filepath.Clean(filepath.FromSlash(key))
	path := strings.Split(key, string(filepath.Separator))
	last := len(path) - 1
	return &diskv.PathKey{Path: path[:last], FileName: path[last]}
}

func pathKeyToKey(pk *diskv.PathKey) string {
	return filepath.ToSlash(filepath.Join(append(pk.Path, pk.FileName)...))
}

// Backend provides a diskv storage backend.
type Backend struct {
	kv *diskv.Diskv
}

// NewBackend provides a new backend instance.
func NewBackend(rootPath string) *Backend {
	return &Backend{diskv.New(diskv.Options{
		BasePath:          filepath.Join(rootPath, "kv"),
		AdvancedTransform: keyToPathKey,
		InverseTransform:  pathKeyToKey,
		Compression:       nil,
		// CacheSizeMax:      8e+7,
		// PathPerm:          0755,
		// FilePerm:          0644,
	})}
}

// GetRootPath returns the filepath location of on disk storage.
func (be *Backend) GetRootPath() string {
	return be.kv.BasePath
}

// Close closes any open items.
func (be *Backend) Close() error {
	return nil
}

// Import ingests a file on the local filesystem and stores it at the specified key.
//
// If move is true, the file will be removed after import.
func (be *Backend) Import(srcPath, key string, move bool) error {
	return be.kv.Import(srcPath, key, move)
}

// Keys returns a list of all keys under the provided prefix.
func (be *Backend) Keys(prefix string) ([]string, error) {
	var keys []string
	for key := range be.kv.KeysPrefix(prefix, nil) {
		keys = append(keys, key)
	}
	return keys, nil
}

// Has checks for the presence of arbitrary data.
func (be *Backend) Has(key string) bool {
	return be.kv.Has(key)
}

// Set stores arbitrary data.
func (be *Backend) Set(key string, r io.Reader) error {
	return be.kv.WriteStream(key, r, false)
}

// Get fetches arbitrary data.
func (be *Backend) Get(key string) (io.ReadCloser, error) {
	return be.kv.ReadStream(key, false)
}

// Delete removes arbitrary data.
func (be *Backend) Delete(key string) error {
	return be.kv.Erase(key)
}
