package service

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestVideoReferencesValidateSelectedMediaBeforeBilling(t *testing.T) {
	for _, adapter := range []string{"zex_video", "volcengine_seedance_2", "topenrouter_seedance_2", "minimax_h3_v2"} {
		t.Run(adapter, func(t *testing.T) {
			model := &ModelFull{ModelDTO: ModelDTO{Code: "seedance-2.0"}, RuntimeRule: map[string]interface{}{"upstream": map[string]interface{}{"adapter": adapter}}}
			cfg := parseVideoRuntimeConfig(model.RuntimeRule)
			params := map[string]interface{}{"generation_mode": "image_video_audio", "reference_images": []string{"i1", "i2"}, "reference_videos": []string{"v1"}, "reference_audios": []string{"a1"}}
			for _, prompt := range []string{"@图片2参考@video1和@音频1", "@image1@image2", "x@image999.com", "<Picture 2> moves"} {
				params["prompt"] = prompt
				if err := validateVideoPromptReferences(model, cfg, params); err != nil {
					t.Fatalf("valid %q: %v", prompt, err)
				}
			}
			for _, prompt := range []string{"@图片0", "@视频2", "@audio?", "@image99999999999999999999999", "@image1@image99", "@image?@audio99"} {
				params["prompt"] = prompt
				if err := validateVideoPromptReferences(model, cfg, params); err == nil {
					t.Fatalf("accepted invalid %q", prompt)
				}
			}
			params["prompt"], params["generation_mode"] = "@图片1", "text"
			if err := validateVideoPromptReferences(model, cfg, params); err == nil {
				t.Fatal("hidden media accepted in text mode")
			}
			params["generation_mode"], params["first_frame"], params["last_frame"], params["prompt"] = "first_last", "first", "last", "@image2"
			if err := validateVideoPromptReferences(model, cfg, params); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestVideoAgentRejectsInvalidReferencesBeforeBillingDatabase(t *testing.T) {
	dsn := os.Getenv("MEDIA_BILLING_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MEDIA_BILLING_TEST_DATABASE_URL for isolated video reference regression")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test database configuration")
	}
	cfg.MaxConns = 1
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `
		CREATE TEMP TABLE models (
			id bigint, code text, display_name text, new_api_model text, new_api_endpoint text,
			request_mode text, category text, icon_url text, description text, tags jsonb,
			input_schema jsonb, default_params jsonb, new_api_extra_params jsonb, price_rule jsonb,
			runtime_rule jsonb, retention_days int, is_enabled bool, sort_order int);
		CREATE TEMP TABLE model_routes (model_id bigint, is_enabled bool);
		CREATE TEMP TABLE workflow_definitions (
			id bigint, code text, name text, description text, icon text, category text,
			nodes jsonb, input_schema jsonb, price_rule jsonb, display_config jsonb, runtime_config jsonb, is_enabled bool);
		INSERT INTO models VALUES (1,'seedance-2.0','Seedance','seedance-2.0','/v1/videos','video','video',NULL,NULL,
			'[]','{}','{}','{}','{"billing_type":"per_request","unit_price":6}',
			'{"upstream":{"adapter":"zex_video"},"video":{"upload_profile":"gateway_reference","max_reference_images":9}}',7,true,0);
		INSERT INTO workflow_definitions VALUES (1,'reference-test','Test',NULL,NULL,'video','[]','{}','{}','{}',
			'{"generation_model_code":"seedance-2.0","generation_type":"video"}',true);`)
	if err != nil {
		t.Fatal(err)
	}
	// A nil billing service ensures invalid input cannot reach a reservation.
	agent := NewAgentService(pool, nil, nil, nil)
	params := map[string]interface{}{"prompt": "@图片1@图片99", "generation_mode": "reference", "reference_images": []string{"https://e.test/image"}}
	if _, err = agent.CreateProject(ctx, 1, "reference-test", params); err == nil || !strings.Contains(err.Error(), "素材引用") {
		t.Fatalf("agent did not reject before billing: %v", err)
	}
	tasks := &TaskService{db: pool, models: NewModelService(pool)}
	if _, err = tasks.Create(ctx, 1, CreateTaskInput{ModelCode: "seedance-2.0", Prompt: "@图片99", Params: params}); err == nil || !strings.Contains(err.Error(), "素材引用") {
		t.Fatalf("task did not reject before billing: %v", err)
	}
	params["prompt"] = "@图片1"
	if err = agent.validateWorkflowVideoReferences(ctx, map[string]interface{}{"generation_model_code": "seedance-2.0", "generation_type": "video"}, params); err != nil {
		t.Fatal(err)
	}
}

func TestVideoReferenceValidationRunsInTaskNormalization(t *testing.T) {
	model := &ModelFull{ModelDTO: ModelDTO{Code: "seedance-2.0"}, RequestMode: "video", RuntimeRule: map[string]interface{}{"upstream": map[string]interface{}{"adapter": "zex_video"}, "video": map[string]interface{}{"upload_profile": "gateway_reference", "max_reference_images": 9}}}
	params := map[string]interface{}{"prompt": "参考@图片2", "generation_mode": "reference", "reference_images": []string{"https://e.test/image"}}
	if err := NormalizeMediaTaskParams(model, params); err == nil || !strings.Contains(err.Error(), "素材引用") {
		t.Fatalf("task normalization did not reject the missing reference: %v", err)
	}
	params["prompt"] = "参考@图片1"
	if err := NormalizeMediaTaskParams(model, params); err != nil {
		t.Fatal(err)
	}
}
