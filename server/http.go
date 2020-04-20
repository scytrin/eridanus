package server

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"go.chromium.org/luci/common/data/stringset"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/riff"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/tiff/lzw"
	_ "golang.org/x/image/vector"
	_ "golang.org/x/image/vp8"
	_ "golang.org/x/image/vp8l"
	_ "golang.org/x/image/webp"
)

var cfgRouter sync.Once

func init() {
	gin.DisableConsoleColor()
}

func staticHTML(tmpl string) gin.HandlerFunc {
	return func(c *gin.Context) { c.HTML(200, tmpl, nil) }
}

func (s *Server) storagePathCheck(c *gin.Context) {
	if s.SaveUploads && s.Storage.Path == "" {
		c.AbortWithError(http.StatusPreconditionFailed,
			errors.New("no configured storage path"))
		return
	}
	c.Next()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.cfgRouter.Do(func() {
		router := gin.Default()
		router.SetFuncMap(template.FuncMap{
			"thumbnail": s.thumbnail,
		})
		router.LoadHTMLGlob("./server/html/*.html")

		router.StaticFile("/favicon.ico", "./static/favicon.ico")
		router.StaticFS("/static/", gin.Dir("./static/", false))
		router.GET("/", staticHTML("index.html"))

		router.GET("/similar", s.similarHandler)

		imgRouter := router.Group("")
		imgRouter.GET("/image/:idHash", s.imagesHandler)
		imgRouter.GET("/image/:idHash/similar", s.similarHandler)
		imgRouter.GET("/image/:idHash/tags", s.tagsHandler)

		spRouter := router.Group("", s.storagePathCheck)
		spRouter.GET("/fetch", staticHTML("fetch.html"))
		spRouter.POST("/fetch", s.fetchPostHandler)
		spRouter.GET("/upload", staticHTML("upload.html"))
		spRouter.POST("/upload", s.uploadPostHandler)

		s.router = router
	})
	s.router.ServeHTTP(w, r)
	//* Leaving in place as this looks to be a really clean way to support GRPC
	// if s.cfg.GRPCServer != nil {
	//   for serviceName := range s.cfg.GRPCServer.GetServiceInfo() {
	//     serveMux.Handle(fmt.Sprintf("/%s/", serviceName), s.cfg.GRPCServer)
	//   }
	// }
}

func (s *Server) thumbnail(idHash string, info ...string) (gin.H, error) {
	altText := bytes.NewBuffer(nil)
	for _, i := range info {
		fmt.Fprintln(altText, i)
	}
	if tags, err := s.GetTags(idHash); err != nil {
		log.Error(err)
		fmt.Fprintln(altText, "unable to fetch tags")
	} else {
		sort.Strings(tags)
		for _, tag := range tags {
			switch {
			case strings.HasPrefix(tag, "size:"):
				sP := strings.Split(tag, ":")
				sizeB, err := strconv.Atoi(sP[len(sP)-1])
				if err != nil {
					log.Error(err)
					continue
				}
				fmt.Fprintln(altText, "size:", humanize.Bytes(uint64(sizeB)))
			case strings.HasPrefix(tag, "dimensions:"):
				dP := strings.Split(tag, ":")
				fmt.Fprintln(altText, "dimensions:", dP[len(dP)-1])
			}
		}
	}

	return gin.H{
		"Path":  fmt.Sprintf("/image/%s", idHash),
		"Alt":   idHash,
		"Title": altText.String(),
	}, nil
}

func (s *Server) tagsHandler(c *gin.Context) {
	idHash := c.Param("idHash")
	tags, ok := s.Cache[idHash]
	if !ok {
		c.AbortWithStatus(404)
		return
	}
	c.IndentedJSON(200, tags)
}

func (s *Server) fetchPostHandler(c *gin.Context) {
	fetchURL, err := url.Parse(strings.TrimSpace(c.Request.FormValue("fetch")))
	if err != nil {
		log.Error(err)
		c.AbortWithError(http.StatusBadRequest,
			errors.New("invalid url"))
		return
	}

	if err := s.Fetcher.Fetch(c.Request.Context(), fetchURL); err != nil {
		log.Error(err)
		c.AbortWithError(http.StatusInternalServerError,
			errors.New("failed fetch"))
		return
	}
}

func (s *Server) similarHandler(c *gin.Context) {
	if c.Request.FormValue("forceRebuild") == "true" {
		s.Similar = nil
	}

	if s.Similar == nil {
		similar, err := buildSimilarImages(s.Cache, 1, 5)
		if err != nil {
			c.AbortWithError(500, err)
			return
		}
		s.Similar = similar
	}

	var keys []string
	idHashParam := c.Param("idHash")
	if idHashParam != "" {
		keys = append(keys, idHashParam)
	} else {
		keys = s.Similar.ByQuantity()
	}

	seen := make(stringset.Set)
	var similar [][]string
	for _, idHash := range keys {
		if len(similar) > 4 {
			break
		}
		if seen.Has(idHash) {
			continue
		}

		showHashes := append([]string{idHash}, s.Similar.ByDistance(idHash)...)
		similar = append(similar, showHashes)
		seen.AddAll(showHashes)
	}

	c.HTML(http.StatusOK, "similar.html", gin.H{
		"Count":   s.Similar.Len(),
		"Similar": similar,
	})
}

func (s *Server) imagesHandler(c *gin.Context) {
	idHash := c.Param("idHash")
	if idHash == "" {
		c.AbortWithStatus(404)
		return
	}
	filePath, err := s.FindImage(idHash)
	if err != nil {
		c.AbortWithError(500, err)
		return
	}
	if filePath == "" {
		c.AbortWithStatus(404)
		return
	}
	c.File(filePath)
}

// https://zupzup.org/go-http-file-upload-download/
func (s *Server) uploadPostHandler(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		c.AbortWithError(500, err)
		return
	}

	var uploaded []string
	for name, headers := range form.File {
		log.Info(name)
		for _, header := range headers {
			log.Info(header.Filename)
			fd, err := header.Open()
			if err != nil {
				log.Error(err)
				continue
			}
			idHash, tags, err := s.Storage.Ingest(fd, false)
			if err != nil {
				log.Error(err)
				continue
			}
			tags = append(tags,
				fmt.Sprintf("filename:%s", header.Filename))
			if err := s.AddTags(idHash, tags...); err != nil {
				log.Errorf("ingest: %v", err)
			}
			uploaded = append(uploaded, idHash)
		}
	}

	c.HTML(http.StatusOK, "uploaded.html", gin.H{
		"Uploaded": uploaded,
	})
}
