package server

import (
	"bytes"
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

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

func init() {
	gin.DisableConsoleColor()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.router == nil {
		router := gin.Default()
		router.StaticFile("/", "./static/index.html")
		router.StaticFile("/favicon.ico", "./static/favicon.ico")
		router.StaticFS("/static/", gin.Dir("./static/", true))

		router.GET("/fetch", gin.WrapF(s.fetchGetHandler))
		router.POST("/fetch", gin.WrapF(s.fetchPostHandler))

		router.GET("/upload", gin.WrapF(s.uploadGetHandler))
		router.POST("/upload", gin.WrapF(s.uploadPostHandler))

		router.GET("/similar", s.similarHandler)

		router.GET("/image/:idHash", s.imagesHandler)
		router.GET("/image/:idHash/similar", s.similarHandler)
		router.GET("/image/:idHash/tags", s.tagsHandler)
		s.router = router
	}
	s.router.ServeHTTP(w, r)
	//* Leaving in place as this looks to be a really clean way to support GRPC
	// if s.cfg.GRPCServer != nil {
	//   for serviceName := range s.cfg.GRPCServer.GetServiceInfo() {
	//     serveMux.Handle(fmt.Sprintf("/%s/", serviceName), s.cfg.GRPCServer)
	//   }
	// }
}

func (s *Server) tagsHandler(c *gin.Context) {
	idHash := c.Param("idHash")
	tags, ok := s.Cache[idHash]
	if !ok {
		c.AbortWithStatus(404)
		// http.NotFound(c.Writer, c.Request)
		return
	}
	c.IndentedJSON(200, tags)
}

func (s *Server) fetchGetHandler(w http.ResponseWriter, r *http.Request) {
	if s.StoragePath == "" {
		http.Error(w, "no configured storage path", http.StatusPreconditionFailed)
		return
	}

	fmt.Fprint(w, `<!doctype html>
<html>
  <head>
    <title>Eridanus</title>
    <link rel="stylesheet" href="/static/style.css" />
    <script src="/static/eridanus.js"></script>
  </head>
  <body>
    <form method="get" enctype="multipart/form-data">
      <input type="text" name="fetch" />
      <input type="submit" />
    </form>
  </body>
</html>`)
}

func (s *Server) fetchPostHandler(w http.ResponseWriter, r *http.Request) {
	if s.StoragePath == "" {
		http.Error(w, "no configured storage path", http.StatusPreconditionFailed)
		return
	}

	fetchURL, err := url.Parse(strings.TrimSpace(r.FormValue("fetch")))
	if err != nil {
		log.Error(err)
		http.Error(w, "invalid url", http.StatusBadRequest)
		return
	}

	if err := s.Fetcher.Fetch(r.Context(), fetchURL); err != nil {
		log.Error(err)
		http.Error(w, "failed fetch", http.StatusBadRequest)
		return
	}
}

func (s *Server) thumbnail(w io.Writer, idHash string, info ...string) error {
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
	fmt.Fprintf(w,
		`<div class="thumbnail"><a href="%[2]s"><img src="%[2]s" alt="%[1]s" title="%[3]s" /></a></div>`,
		idHash, fmt.Sprintf("/image/%s", idHash), altText.String())

	return nil
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

	var shown int
	seen := make(stringset.Set)
	body := bytes.NewBuffer(nil)
	for _, idHash := range keys {
		if seen.Has(idHash) {
			continue
		}

		showHashes := append([]string{idHash}, s.Similar.ByDistance(idHash)...)
		seen.AddAll(showHashes)
		fmt.Fprint(body, `<div class="thumbnails">`)
		for _, sh := range showHashes {
			if err := s.thumbnail(body, sh,
				fmt.Sprintf("...%s", sh[len(sh)-6:]),
				fmt.Sprintf("dist: %s", humanize.Ftoa(s.Similar.Distance(idHash, sh))),
			); err != nil {
				log.Error(err)
			}
		}
		fmt.Fprint(body, `</div>`)

		shown++
		if 5 < shown {
			break
		}
	}

	// look to using c.HTML after router.LoadHTMLFiles
	c.Data(200, "text/html", []byte(fmt.Sprintf(`<!doctype html>
<html>
  <head>
    <title>Eridanus</title>
    <link rel="stylesheet" href="/static/style.css" />
    <script src="/static/eridanus.js"></script>
  </head>
  <body>
    <h1>Similar files</h1>
    <h2>%d cases</h2>
    %s
  </body>
</html>`, s.Similar.Len(), body)))
}

func (s *Server) imagesHandler(c *gin.Context) {
	idHash := c.Param("idHash")
	if idHash == "" {
		c.AbortWithStatus(404)
		return
	}
	filePath, err := s.findImage(idHash)
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
func (s *Server) uploadGetHandler(w http.ResponseWriter, r *http.Request) {
	if s.StoragePath == "" {
		http.Error(w, "no configured storage path", http.StatusPreconditionFailed)
		return
	}

	fmt.Fprint(w, `<!doctype html>
<html>
  <head>
    <title>Eridanus</title>
    <link rel="stylesheet" href="/static/style.css" />
    <script src="/static/eridanus.js"></script>
  </head>
  <body>
    <form method="post" enctype="multipart/form-data">
      <input type="file" name="files" multiple="multiple" />
      <input type="submit" />
    </form>
  </body>
</html>`)
}

// https://zupzup.org/go-http-file-upload-download/
func (s *Server) uploadPostHandler(w http.ResponseWriter, r *http.Request) {
	if s.StoragePath == "" {
		http.Error(w, "no configured storage path", http.StatusPreconditionFailed)
		return
	}

	mr, err := r.MultipartReader()
	if err != nil {
		log.Errorf("file upload err (reader): %v", err)
		return
	}

	body := bytes.NewBuffer(nil)
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Error(err)
			break
		}
		defer part.Close()

		if part.FormName() != "files" {
			continue
		}

		idHash, err := s.IngestFile(filepath.Base(part.FileName()), part, true)
		if err != nil {
			log.Error(err)
			continue
		}

		fmt.Fprintf(body, "<div><a href='/image/%[1]s'>%[1]s</a></div>\n", idHash)
	}

	fmt.Fprintf(w, `<!doctype html>
<html>
  <head>
    <title>Eridanus</title>
    <link rel="stylesheet" href="/static/style.css" />
    <script src="/static/eridanus.js"></script>
  </head>
  <body>
    <h1>Uploaded files</h1>
    %s
  </body>
</html>`, body)
}
