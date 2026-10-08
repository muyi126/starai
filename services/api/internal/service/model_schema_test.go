package service

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestModelSchemaDefaultsDriveWorkbenchAndBilling(t *testing.T) {
	for _, category := range []string{"chat", "image", "video", "audio"} {
		t.Run(category, func(t *testing.T) {
			input := CreateModelInput{Category: category, DefaultParams: map[string]interface{}{"duration": float64(5), "resolution": "480p", "generate_audio": true, "channel_key": "keep"}}
			if err := json.Unmarshal([]byte(`{"required":["prompt"],"properties":{"duration":{"type":"integer","enum":[5,10],"default":10},"resolution":{"type":"string","enum":["480p","720p"],"default":"720p"},"generate_audio":{"type":"boolean","default":false},"temperature":{"type":"number","minimum":0,"maximum":2,"multipleOf":0.25,"default":0.75}}}`), &input.InputSchema); err != nil {
				t.Fatal(err)
			}
			old := copyMap(input.DefaultParams)
			previous := map[string]interface{}{}
			_ = json.Unmarshal([]byte(`{"properties":{"duration":{"default":5},"resolution":{"default":"480p"},"generate_audio":{"default":true}}}`), &previous)
			if err := normalizeModelSchemaDefaults(&input, previous); err != nil {
				t.Fatal(err)
			}
			if input.DefaultParams["duration"] != float64(10) || input.DefaultParams["resolution"] != "720p" || input.DefaultParams["generate_audio"] != false || input.DefaultParams["channel_key"] != "keep" {
				t.Fatalf("defaults not synchronized: %#v", input.DefaultParams)
			}
			if !reflect.DeepEqual(old, map[string]interface{}{"duration": float64(5), "resolution": "480p", "generate_audio": true, "channel_key": "keep"}) {
				t.Fatal("original defaults were mutated")
			}
			model := &ModelFull{ModelDTO: ModelDTO{PriceRule: map[string]interface{}{"billing_type": "per_second", "unit_price_by_resolution": map[string]interface{}{"480p": float64(1), "720p": float64(2)}}}}
			if got := NewModelService(nil).EstimateCost(model, MergeMediaTaskParams(input.DefaultParams, nil), 0, 0); got != 20 {
				t.Fatalf("quote did not use the selected defaults: %v", got)
			}
		})
	}
}

// Run against a migrated disposable database; never makes upstream requests.
func TestModelSchemaCreateAndUpdatePersistDefaults(t *testing.T) {
	dsn := os.Getenv("MODEL_SCHEMA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MODEL_SCHEMA_TEST_DATABASE_URL to a migrated disposable database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := NewModelService(pool)
	for category, mode := range map[string]string{"chat": "chat_completions", "image": "images", "video": "video", "audio": "audio"} {
		t.Run(category, func(t *testing.T) {
			input := CreateModelInput{Code: "schema-audit-" + category, DisplayName: "Schema audit", Category: category, RequestMode: mode, NewAPIModel: "test-model", IsEnabled: true,
				NewAPIExtraParams: map[string]interface{}{"connection": map[string]interface{}{"base_url": "https://example.test", "auth_type": "none"}},
				PriceRule:         map[string]interface{}{"billing_type": "per_request", "unit_price": float64(1)},
				DefaultParams:     map[string]interface{}{},
			}
			_ = json.Unmarshal([]byte(`{"properties":{"resolution":{"type":"string","enum":["480p","720p"],"default":"720p","title":"画质"}}}`), &input.InputSchema)
			created, err := svc.Create(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if created.DefaultParams["resolution"] != "720p" {
				t.Fatalf("creation persisted stale defaults: %#v", created.DefaultParams)
			}
			field := input.InputSchema["properties"].(map[string]interface{})["resolution"].(map[string]interface{})
			field["default"] = "480p"
			input.DefaultParams = created.DefaultParams
			updated, err := svc.Update(ctx, created.ID, input)
			if err != nil {
				t.Fatal(err)
			}
			full, err := svc.GetFullByCode(ctx, input.Code)
			if err != nil || updated.DefaultParams["resolution"] != "480p" || full.DefaultParams["resolution"] != "480p" {
				t.Fatalf("updated defaults not available to workbench/tasks: %#v, %v", updated, err)
			}
			field["default"] = "invalid"
			if _, err := svc.Update(ctx, created.ID, input); err == nil {
				t.Fatal("invalid default was accepted")
			}
			unchanged, err := svc.GetByID(ctx, created.ID)
			if err != nil || unchanged.DefaultParams["resolution"] != "480p" {
				t.Fatal("invalid update changed the saved defaults")
			}
		})
	}
}

func TestModelSchemaDefaultsRejectInvalidConfiguration(t *testing.T) {
	for _, field := range []string{
		`{"type":"integer","enum":[]}`,
		`{"type":"integer","minimum":10,"maximum":5}`,
		`{"type":"number","multipleOf":0}`,
		`{"type":"string","enum":["text","image"],"default":"bad"}`,
		`{"type":"integer","minimum":1,"maximum":9,"default":10}`,
		`{"type":"number","multipleOf":0.5,"default":0.3}`,
		`{"type":"boolean","default":"false"}`,
		`{"type":"string","maxLength":3,"default":"too long"}`,
	} {
		var schema map[string]interface{}
		_ = json.Unmarshal([]byte(`{"properties":{"setting":`+field+`}}`), &schema)
		if err := normalizeModelSchemaDefaults(&CreateModelInput{InputSchema: schema}); err == nil {
			t.Fatalf("accepted invalid default: %s", field)
		}
	}
	input := CreateModelInput{DefaultParams: map[string]interface{}{"duration": float64(5)}, InputSchema: map[string]interface{}{"properties": map[string]interface{}{"duration": map[string]interface{}{"type": "integer", "enum": []interface{}{float64(4), float64(5)}}}}}
	if err := normalizeModelSchemaDefaults(&input); err != nil || input.DefaultParams["duration"] != float64(5) {
		t.Fatalf("enum fallback overrode an explicit default: %#v, %v", input.DefaultParams, err)
	}
	input.InputSchema["properties"].(map[string]interface{})["duration"].(map[string]interface{})["default"] = float64(4)
	if err := normalizeModelSchemaDefaults(&input, copyMap(input.InputSchema)); err != nil || input.DefaultParams["duration"] != float64(5) {
		t.Fatalf("unchanged schema overrode visual editor defaults: %#v, %v", input.DefaultParams, err)
	}
}
