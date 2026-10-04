package main

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDolaGenerationAttemptPreservesLegacyMultipartContract(t *testing.T) {
	png := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'})
	jpg := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte{0xff, 0xd8, 0xff, 0xe0, 0, 16, 'J', 'F', 'I', 'F', 0})
	for _, refs := range [][]string{nil, {png, jpg}} {
		name := "text-only"
		if len(refs) > 0 {
			name = "reference-images"
		}
		t.Run(name, func(t *testing.T) {
			calls, polls := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					polls++
					if r.URL.Path != "/api/v1/videos/dola-task" || r.Header.Get("Authorization") != "Bearer fixture-key" || r.Header.Get("Idempotency-Key") != "legacy-task" {
						t.Errorf("poll path or authentication/idempotency contract changed")
					}
					_, _ = w.Write([]byte(`{"code":"1","status":"completed","video_url":"https://example.test/dola.mp4"}`))
					return
				}
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/api/v1/videos" || !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
					t.Errorf("method=%s path=%s content-type=%s", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
				}
				if r.Header.Get("Authorization") != "Bearer fixture-key" || r.Header.Get("Idempotency-Key") != "legacy-task" {
					t.Errorf("authentication or idempotency header missing")
				}
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Errorf("parse multipart: %v", err)
					return
				}
				defer r.MultipartForm.RemoveAll()
				want := map[string][]string{"prompt": {"city sunrise"}, "ratio": {"16:9"}, "seconds": {"30"}}
				if !reflect.DeepEqual(r.MultipartForm.Value, want) {
					t.Errorf("fields=%#v", r.MultipartForm.Value)
				}
				files := r.MultipartForm.File["images[]"]
				if len(files) != len(refs) {
					t.Errorf("files=%d want=%d", len(files), len(refs))
				}
				for i, f := range files {
					if f.Header.Get("Content-Type") != []string{"image/png", "image/jpeg"}[i] {
						t.Errorf("image type=%s", f.Header.Get("Content-Type"))
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"code":"1","task_id":"dola-task","status":"queued"}`))
			}))
			defer server.Close()
			rule := map[string]interface{}{"upstream": map[string]interface{}{"adapter": "dola_seedance_30s", "include": []interface{}{"duration", "ratio", "reference_images"}, "request_timeout_sec": 5, "poll_path": "/api/v1/videos/{id}", "poll_interval_sec": 10, "poll_timeout_sec": 1800}}
			route := workerModelRoute{Endpoint: "/api/v1/videos", RuntimeRule: rule, Connection: connectionConfig{BaseURL: server.URL, APIKey: "fixture-key", AuthType: "bearer", APIKeyHeader: "Authorization"}}
			p := ImageTaskPayload{TaskNo: "legacy-task", ModelCode: "custom-dola", Input: map[string]interface{}{"prompt": "city sunrise", "ratio": "16:9", "duration": 30, "reference_images": refs, "_price_rule_snapshot": map[string]interface{}{"unit_price": 1}}}
			got, err := executeWorkerGenerationAttempt(context.Background(), nil, p, route, true, false, false, "city sunrise")
			if err != nil || got.StatusCode != http.StatusOK || calls != 1 || got.RequestCount != 1 {
				t.Fatalf("result=%#v calls=%d err=%v", got, calls, err)
			}
			_, id := parseUpstreamMedia(got.ResponseBody)
			if id != "dola-task" {
				t.Fatalf("task id=%q", id)
			}
			cfg := parsePollConfig(rule, route.Endpoint)
			if cfg.Path != "/api/v1/videos/{id}" || cfg.Interval != 10*time.Second || cfg.Timeout != 1800*time.Second {
				t.Fatalf("poll config=%#v", cfg)
			}
			items, _, err := pollUpstreamTask(context.Background(), nil, got.Connection, cfg, id, p.TaskNo)
			if err != nil || polls != 1 || len(items) != 1 || items[0].URL != "https://example.test/dola.mp4" {
				t.Fatalf("polled items=%#v polls=%d err=%v", items, polls, err)
			}
		})
	}
}

func TestDolaRejectsInvalidPayloadBeforePosting(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	for _, tc := range []struct {
		name, key string
		value     interface{}
	}{
		{"empty-prompt", "prompt", ""}, {"long-prompt", "prompt", strings.Repeat("字", 3001)},
		{"invalid-ratio", "ratio", "auto"}, {"wrong-duration", "seconds", "4"},
		{"too-many-images", "reference_images", []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}},
		{"invalid-image-type", "reference_images", []string{"data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte("not an image"))}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]interface{}{"prompt": "sunrise", "ratio": "16:9", "seconds": "30"}
			payload[tc.key] = tc.value
			_, _, err := postVideoUpstream(context.Background(), connectionConfig{BaseURL: server.URL, AuthType: "none"}, "/api/v1/videos", payload, map[string]interface{}{"upstream": map[string]interface{}{"adapter": "dola_seedance_30s"}}, "invalid-task")
			if err == nil {
				t.Fatal("expected payload rejection")
			}
		})
	}
	if calls != 0 {
		t.Fatalf("invalid payload posted %d times", calls)
	}
}

func TestDolaBusinessFailureReturnsGenerationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"message":"fixture quota exhausted"}`))
	}))
	defer server.Close()
	route := workerModelRoute{Endpoint: "/api/v1/videos", RuntimeRule: map[string]interface{}{"upstream": map[string]interface{}{"adapter": "dola_seedance_30s"}}, Connection: connectionConfig{BaseURL: server.URL, AuthType: "none"}}
	p := ImageTaskPayload{TaskNo: "failed-task", Input: map[string]interface{}{"prompt": "sunrise", "ratio": "16:9", "duration": 30}}
	_, err := executeWorkerGenerationAttempt(context.Background(), nil, p, route, true, false, false, "sunrise")
	if err == nil || !strings.Contains(err.Error(), "fixture quota exhausted") {
		t.Fatalf("business error=%v", err)
	}
}
