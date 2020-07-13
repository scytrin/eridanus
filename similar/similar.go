package similar

import (
	"fmt"
	"image"
	"io"
	"math"
	"strings"
	"time"

	"github.com/corona10/goimagehash"
	"github.com/pkg/errors"
	"github.com/scytrin/eridanus"
	"github.com/sirupsen/logrus"
	"gonum.org/v1/gonum/spatial/vptree"
)

func buildHash(s eridanus.Storage, idHash eridanus.IDHash, generate bool) (vptree.Comparable, error) {
	tags, err := s.TagStorage().Get(idHash)
	if err != nil {
		return nil, err
	}

	pHash, err := extractPHashTag(tags)
	if err != nil {
		return nil, err
	}

	if pHash == nil {
		if !generate {
			return nil, errors.Errorf("no phash for %s, generation disallowed", idHash)
		}

		r, err := s.ContentStorage().Get(idHash)
		if err != nil {
			return nil, err
		}

		pHash, err = generatePHashTag(r)
		if err != nil {
			return nil, err
		}

		tags = append(tags, eridanus.Tag(fmt.Sprintf("phash:%s", pHash.ToString())))
		if err := s.TagStorage().Put(idHash, tags); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func buildHashes(s eridanus.Storage, idHashes eridanus.IDHashes, generate bool) ([]vptree.Comparable, error) {
	hashStart := time.Now()
	var hashes []vptree.Comparable

	for _, idHash := range idHashes {
		compHash, err := buildHash(s, idHash, generate)
		if err != nil {
			logrus.Error(err)
			continue
		}
		hashes = append(hashes, compHash)
	}

	logrus.Infof("%s -- %d items -> %d hashes",
		time.Now().Sub(hashStart), len(idHashes), len(hashes))
	return hashes, nil
}

func buildVPTree(hashes []vptree.Comparable, effort int) (*vptree.Tree, error) {
	treeStart := time.Now()
	tree, err := vptree.New(hashes, effort, nil)
	if err != nil {
		return nil, err
	}
	logrus.Infof("%s -- %d hashes -> %d tree",
		time.Now().Sub(treeStart), len(hashes), tree.Len())
	return tree, nil
}

func extractPHashTag(tags eridanus.Tags) (*goimagehash.ImageHash, error) {
	for _, tag := range tags {
		if strings.HasPrefix(string(tag), "phash:") {
			pHash, err := goimagehash.ImageHashFromString(strings.TrimPrefix(string(tag), "phash:"))
			if err != nil {
				return nil, err
			}
			if pHash.GetKind() != goimagehash.PHash {
				return nil, errors.Errorf("phash type mismatch: %v", pHash.GetKind())
			}
			return pHash, nil
		}
	}
	return nil, errors.New("no phash tag found")
}

func generatePHashTag(r io.Reader) (i *goimagehash.ImageHash, err error) {
	defer eridanus.RecoveryHandler(func(e error) { err = e })

	img, _, err := image.Decode(r)
	if err != nil {
		return nil, err
	}

	pHash, err := goimagehash.PerceptionHash(img)
	if err != nil {
		return nil, err
	}

	if pHash.GetHash() == 0 {
		return nil, errors.New("phash generation failed")
	}

	return pHash, nil
}

type cph struct {
	Hash      eridanus.IDHash
	ImageHash *goimagehash.ImageHash
}

// Distance satisfies vptree.Comparable.Distance.
func (h *cph) Distance(ov vptree.Comparable) float64 {
	if o, ok := ov.(*cph); ok {
		if h.Hash == o.Hash || h.ImageHash == o.ImageHash {
			return 0
		}
		if d, err := h.ImageHash.Distance(o.ImageHash); err != nil {
			logrus.Error(err)
		} else {
			return float64(d)
		}
	}
	return math.MaxFloat64
}

func findSimilar(tree *vptree.Tree, target vptree.Comparable, maxDist float64) map[eridanus.IDHash]float64 {
	similar := make(map[eridanus.IDHash]float64)
	qh := target.(*cph).Hash
	d := vptree.NewDistKeeper(maxDist)
	tree.NearestSet(d, target)
	for rr := d.Pop(); d.Len() > 0; rr = d.Pop() {
		rd := rr.(vptree.ComparableDist)
		rh := rd.Comparable.(*cph).Hash
		if qh != rh {
			similar[rh] = rd.Dist
		}
	}
	return similar
}

// Find returns a map of idHash to perceptive distance from the specified target.
func Find(s eridanus.Storage, targetIDHash eridanus.IDHash, effort int, maxDist float64, generate bool) (map[eridanus.IDHash]float64, error) {
	target, err := buildHash(s, targetIDHash, generate)
	if err != nil {
		return nil, err
	}
	idHashes, err := s.ContentStorage().Hashes()
	if err != nil {
		return nil, err
	}
	hashes, err := buildHashes(s, idHashes, generate)
	if err != nil {
		return nil, err
	}
	tree, err := buildVPTree(hashes, effort)
	if err != nil {
		return nil, err
	}
	return findSimilar(tree, target, maxDist), nil
}
