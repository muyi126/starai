package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Leave room for multipart headers and text fields without accepting an unbounded body.
func limitMultipartRequest(c *gin.Context, fileBytes int64) error {
	limit := fileBytes + 1<<20
	if c.Request.ContentLength > limit {
		return errors.New("上传请求过大")
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	return nil
}

func removeMultipartFiles(c *gin.Context) {
	if c.Request.MultipartForm != nil {
		_ = c.Request.MultipartForm.RemoveAll()
	}
}
