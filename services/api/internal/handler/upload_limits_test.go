package handler

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type uploadCountReader struct {
	io.Reader
	read int64
}

func (r *uploadCountReader) Read(b []byte) (int, error) {
	n, err := r.Reader.Read(b)
	r.read += int64(n)
	return n, err
}

type uploadZeroReader struct{}

func (uploadZeroReader) Read(b []byte) (int, error) { clear(b); return len(b), nil }

func TestUploadRequestLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name      string
		fileLimit int64
		run       func(*Handler, *gin.Context)
	}{
		{"image", 10 << 20, (*Handler).Upload},
		{"asset", 20 << 20, (*Handler).UploadAsset},
		{"openapi image edit", 20 * (20 << 20), func(h *Handler, c *gin.Context) { _, _ = h.openAPIImageEditBody(c) }},
	} {
		t.Run(test.name+" rejects known oversized body before reading", func(t *testing.T) {
			r := &uploadCountReader{Reader: uploadZeroReader{}}
			req := httptest.NewRequest("POST", "/upload", io.NopCloser(r))
			req.Header.Set("Content-Type", "multipart/form-data; boundary=fixture")
			req.ContentLength = test.fileLimit + (1 << 20) + 1
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = req
			store := &openAPIImageEditStore{}
			test.run(&Handler{storage: store}, c)
			if recorder.Code != 400 || r.read != 0 || len(store.objects) != 0 {
				t.Fatalf("status=%d read=%d objects=%v", recorder.Code, r.read, store.objects)
			}
		})
		if test.name == "openapi image edit" {
			continue
		}
		t.Run(test.name+" bounds chunked body", func(t *testing.T) {
			header := "--fixture\r\nContent-Disposition: form-data; name=\"file\"; filename=\"image.png\"\r\nContent-Type: image/png\r\n\r\n"
			r := &uploadCountReader{Reader: io.MultiReader(strings.NewReader(header), io.LimitReader(uploadZeroReader{}, test.fileLimit+(2<<20)), strings.NewReader("\r\n--fixture--\r\n"))}
			req := httptest.NewRequest("POST", "/upload", io.NopCloser(r))
			req.ContentLength = -1
			req.Header.Set("Content-Type", "multipart/form-data; boundary=fixture")
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = req
			store := &openAPIImageEditStore{}
			test.run(&Handler{storage: store}, c)
			if recorder.Code != 400 || r.read > test.fileLimit+(1<<20)+1 || len(store.objects) != 0 || !strings.Contains(recorder.Body.String(), "MB") {
				t.Fatalf("status=%d read=%d response=%s objects=%v", recorder.Code, r.read, recorder.Body.String(), store.objects)
			}
		})
	}
}

func TestMultipartCleanupAndAllowedBody(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "image.png")
	_, _ = part.Write([]byte("fixture image"))
	_ = writer.Close()
	req := httptest.NewRequest("POST", "/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	if err := limitMultipartRequest(c, 10<<20); err != nil {
		t.Fatal(err)
	}
	if err := req.ParseMultipartForm(1); err != nil {
		t.Fatal(err)
	}
	file, err := req.MultipartForm.File["file"][0].Open()
	if err != nil {
		t.Fatal(err)
	}
	path := file.(*os.File).Name()
	_ = file.Close()
	removeMultipartFiles(c)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary upload not removed: %v", err)
	}
}
