package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/starai/api/internal/service"
)

// Quotes must enforce the same money-sensitive inputs as task creation, while
// allowing the workbench to quote before the user has entered a prompt.
func TestMediaEstimateHTTPValidatesMergedDefaults(t *testing.T) {
	dsn := os.Getenv("TASK_READ_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TASK_READ_TEST_DATABASE_URL for isolated estimate regression")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf("media_estimate_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") })
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `
		CREATE TABLE models (
			id bigserial PRIMARY KEY, code text, display_name text, new_api_model text,
			new_api_endpoint text, request_mode text, category text, icon_url text,
			description text, tags jsonb DEFAULT '[]', input_schema jsonb,
			default_params jsonb, new_api_extra_params jsonb DEFAULT '{}',
			price_rule jsonb, runtime_rule jsonb DEFAULT '{}', retention_days int DEFAULT 7,
			is_enabled boolean DEFAULT true, sort_order int DEFAULT 0
		);
		CREATE TABLE model_routes (model_id bigint, is_enabled boolean);
		INSERT INTO models (code,display_name,new_api_model,new_api_endpoint,request_mode,category,input_schema,default_params,price_rule)
		VALUES
		('image','Image','image','/v1/images/generations','images','image',
		 '{"required":["prompt"],"properties":{"n":{"type":"integer","minimum":1,"maximum":5}}}',
		 '{"n":1}', '{"billing_type":"per_image","unit_price":2}'),
		('video','Video','video','/v1/video/generations','video','video',
		 '{"required":["prompt"],"properties":{"duration":{"type":"number","minimum":1,"maximum":10}}}',
		 '{"duration":4,"count":1}', '{"billing_type":"per_second","unit_price":0.5}'),
		('overflow','Overflow','image','/v1/images/generations','images','image',
		 '{}', '{"n":1}', '{"billing_type":"per_image","unit_price":1e308}');
	`); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	h := &Handler{models: service.NewModelService(pool)}
	router := gin.New()
	router.POST("/models/:code/estimate", h.EstimateModel)
	for _, tc := range []struct {
		name, model, body string
		status            int
		cost              float64
	}{
		{"default quote without prompt", "image", `{"params":{}}`, 200, 2},
		{"count alias overrides default n", "image", `{"params":{"count":"3"}}`, 200, 6},
		{"seconds alias overrides duration", "video", `{"params":{"seconds":"6s","n":2}}`, 200, 6},
		{"fractional count rejected", "image", `{"params":{"n":0.1}}`, 400, 0},
		{"conflicting counts rejected", "image", `{"params":{"n":2,"count":3}}`, 400, 0},
		{"schema maximum enforced", "image", `{"params":{"n":6}}`, 400, 0},
		{"internal usage rejected", "video", `{"params":{"_actual_output_seconds":0}}`, 400, 0},
		{"price snapshot rejected", "image", `{"params":{"_price_rule_snapshot":{"unit_price":0}}}`, 400, 0},
		{"invalid JSON rejected", "image", `{`, 400, 0},
		{"overflowing quote rejected", "overflow", `{"params":{"n":3}}`, 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/models/"+tc.model+"/estimate", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != tc.status {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if tc.status == 200 {
				var response struct {
					Data struct {
						EstimatedCost float64 `json:"estimated_cost"`
					} `json:"data"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Data.EstimatedCost != tc.cost {
					t.Fatalf("cost=%v want=%v body=%s", response.Data.EstimatedCost, tc.cost, recorder.Body.String())
				}
			}
		})
	}
}
