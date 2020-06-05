package similar

// import (
// 	"fmt"
// 	"image"
// 	"math"
// 	"sort"
// 	"strings"
// 	"sync"
// 	"time"

// 	"github.com/corona10/goimagehash"
// 	log "github.com/sirupsen/logrus"
// 	"gonum.org/v1/gonum/spatial/vptree"
// 	"stadik.net/eridanus"
// )

// var gslSimilar sync.RWMutex

// type goimagehasher func(image.Image) (*goimagehash.ImageHash, error)

// func GeneratePHashTag(img image.Image) ([]string, error) {
// 	var tags []string

// 	defer func() {
// 		if err := recover(); err != nil {
// 			log.Error(err)
// 		}
// 	}()
// 	hsh, err := goimagehash.PerceptionHash(img)
// 	if err != nil {
// 		log.Error(err)
// 	}
// 	if hsh.GetHash() > 0 {
// 		tags = append(tags, fmt.Sprintf("phash:%s", hsh.ToString()))
// 	}

// 	return tags, nil
// }

// type Similar map[string]map[string]float64

// func New(m eridanus.TagDB, effort int, maxDist float64) (Similar, error) {
// 	tree, err := buildVPTree(m, 1) // minimal effort
// 	if err != nil {
// 		return nil, err
// 	}

// 	findStart := time.Now()
// 	similar := make(Similar)
// 	similar.AddAllSimilar(tree, maxDist)
// 	log.Infof("%s -- %d tree => %d similar @ d=%f",
// 		time.Now().Sub(findStart), tree.Len(), similar.Len(), maxDist)

// 	return similar, nil
// }

// func (m Similar) Len() int {
// 	gslSimilar.RLock()
// 	defer gslSimilar.RUnlock()

// 	return len(m)
// }

// func (m Similar) Distance(lh, rh string) float64 {
// 	if lh == rh {
// 		return 0
// 	}
// 	return m[lh][rh]
// }

// func (m Similar) ByQuantity() []string {
// 	var keys []string
// 	for k := range m {
// 		keys = append(keys, k)
// 	}
// 	sort.Strings(keys)
// 	sort.SliceStable(keys, func(i, j int) bool {
// 		return len(m[keys[i]]) > len(m[keys[j]])
// 	})
// 	return keys
// }

// func (m Similar) ByDistance(idHash string) []string {
// 	var keys []string
// 	for rh := range m[idHash] {
// 		keys = append(keys, rh)
// 	}
// 	sort.Strings(keys)
// 	sort.SliceStable(keys, func(i, j int) bool {
// 		return m[idHash][keys[i]] < m[idHash][keys[j]]
// 	})
// 	return keys
// }

// func (m Similar) Add(lh, rh string, d float64) error {
// 	if lh == rh {
// 		return nil
// 	}

// 	gslSimilar.Lock()
// 	defer gslSimilar.Unlock()

// 	for _, h1 := range []string{lh, rh} {
// 		if _, ok := m[h1]; !ok {
// 			m[h1] = make(map[string]float64)
// 		}
// 	}
// 	m[lh][rh] = d
// 	m[rh][lh] = d
// 	return nil
// }

// func (m Similar) AddSimilar(tree *vptree.Tree, maxDist float64, q vptree.Comparable) error {
// 	qh := q.(*cph).Hash
// 	d := vptree.NewDistKeeper(maxDist)
// 	tree.NearestSet(d, q)
// 	for rr := d.Pop(); d.Len() > 0; rr = d.Pop() {
// 		rd := rr.(vptree.ComparableDist)
// 		rh := rd.Comparable.(*cph).Hash
// 		if qh != rh {
// 			m.Add(qh, rh, rd.Dist)
// 		}
// 	}
// 	return nil
// }

// func (m Similar) AddAllSimilar(tree *vptree.Tree, maxDist float64) error {
// 	var wg sync.WaitGroup
// 	tree.Do(func(q vptree.Comparable, _ int) bool {
// 		wg.Add(1)
// 		go func() {
// 			defer wg.Done()
// 			if err := m.AddSimilar(tree, maxDist, q); err != nil {
// 				log.Error(err)
// 			}
// 		}()
// 		return false
// 	})
// 	wg.Wait()
// 	return nil
// }

// func buildVPTree(m eridanus.TagDB, effort int) (*vptree.Tree, error) {
// 	hashes, err := buildHashes(m)
// 	if err != nil {
// 		return nil, err
// 	}
// 	treeStart := time.Now()
// 	tree, err := vptree.New(hashes, effort, nil)
// 	if err != nil {
// 		return nil, err
// 	}
// 	log.Infof("%s -- %d hashes -> %d tree",
// 		time.Now().Sub(treeStart), len(hashes), tree.Len())
// 	return tree, nil
// }

// func buildHashes(m eridanus.TagDB) ([]vptree.Comparable, error) {
// 	hashStart := time.Now()
// 	var hashes []vptree.Comparable
// 	if err := m.Range(func(idHash string, tags []string) error {
// 		for _, tag := range tags {
// 			if strings.HasPrefix(tag, "phash:") {
// 				pHash, err := goimagehash.ImageHashFromString(
// 					strings.TrimPrefix(tag, "phash:"))
// 				if err != nil {
// 					log.Error(err)
// 					continue
// 				}
// 				if pHash.GetKind() != goimagehash.PHash {
// 					continue
// 				}
// 				hashes = append(hashes, &cph{Hash: idHash, ImageHash: pHash})
// 			}
// 		}
// 		return nil
// 	}); err != nil {
// 		return nil, err
// 	}
// 	log.Infof("%s -- %d items -> %d hashes",
// 		time.Now().Sub(hashStart), m.Len(), len(hashes))
// 	return hashes, nil
// }

// func findComparable(t *vptree.Tree, idHash string) vptree.Comparable {
// 	var r vptree.Comparable
// 	t.Do(func(q vptree.Comparable, _ int) bool {
// 		if idHash == q.(*cph).Hash {
// 			r = q
// 			return true
// 		}
// 		return false
// 	})
// 	return r
// }

// type cph struct {
// 	Hash      string
// 	ImageHash *goimagehash.ImageHash
// }

// func (h *cph) Distance(ov vptree.Comparable) float64 {
// 	if o, ok := ov.(*cph); ok {
// 		if h.Hash == o.Hash || h.ImageHash == o.ImageHash {
// 			return 0
// 		}
// 		if d, err := h.ImageHash.Distance(o.ImageHash); err != nil {
// 			log.Error(err)
// 		} else {
// 			return float64(d)
// 		}
// 	}
// 	return math.MaxFloat64
// }
