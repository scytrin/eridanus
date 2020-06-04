package eridanus

import (
	"fmt"
	"hash/crc32"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/improbable-eng/go-httpwares/logging/logrus/ctxlogrus"
)

func init() {
	gin.DisableConsoleColor()
	gin.SetMode(gin.ReleaseMode)
}

func NewRouter(ingest IngestFunc) *gin.Engine {
	router := gin.Default()

	router.Use(func(c *gin.Context) {
		ctx := c.Request.Context()
		reqID := crc32.ChecksumIEEE([]byte(fmt.Sprint(c.Request.Header)))
		log := ctxlogrus.Extract(ctx).WithField("reqID", reqID)
		c.Request = c.Request.WithContext(ctxlogrus.ToContext(ctx, log))
		c.Next()
	})

	router.StaticFile("/", "./static/index.html")
	router.StaticFS("/static/", gin.Dir("./static/", false))

	imgRouter := router.Group("")
	imgRouter.GET("/image/:idHash", func(c *gin.Context) {})
	imgRouter.GET("/image/:idHash/tags", func(c *gin.Context) {})

	spRouter := router.Group("")
	spRouter.POST("/fetch", func(c *gin.Context) {})
	spRouter.POST("/upload", func(c *gin.Context) {
		ctx := c.Request.Context()
		log := ctxlogrus.Extract(ctx)

		form, err := c.MultipartForm()
		if err != nil {
			c.AbortWithError(500, err)
			return
		}

		var uploaded []string
		log.Infof("%d uploaded", len(form.File["file"]))
		for _, header := range form.File["file"] {
			fd, err := header.Open()
			if err != nil {
				log.Error(err)
				continue
			}
			idHash, err := ingest(ctx, fd,
				fmt.Sprint("source:upload"),
				fmt.Sprintf("filename:%s", header.Filename))
			if err != nil {
				log.Error(err)
				continue
			}
			uploaded = append(uploaded, idHash)
		}

		log.Info(uploaded)
		c.JSON(http.StatusOK, uploaded)
	})

	return router
}
