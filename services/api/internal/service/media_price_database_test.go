package service

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMediaModelLoadNormalizesLegacyPriceBeforeSnapshot(t *testing.T) {
	dsn := os.Getenv("MEDIA_BILLING_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MEDIA_BILLING_TEST_DATABASE_URL for isolated media price checks")
	}
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("test database configuration failed")
	}
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("test database pool failed")
	}
	t.Cleanup(pool.Close)
	// Connection-local tables cannot modify the application's existing tables.
	_, err = pool.Exec(ctx, `
		CREATE TEMP TABLE models (
			id BIGINT, code TEXT, display_name TEXT, new_api_model TEXT, new_api_endpoint TEXT,
			request_mode TEXT, category TEXT, icon_url TEXT, description TEXT, tags JSONB,
			input_schema JSONB, default_params JSONB, new_api_extra_params JSONB,
			price_rule JSONB, runtime_rule JSONB, retention_days INT, is_enabled BOOL, sort_order INT
		);
		CREATE TEMP TABLE model_routes (model_id BIGINT, is_enabled BOOL);
		INSERT INTO models VALUES (1,'legacy','Legacy','legacy','/v1/video/generations','video','video',NULL,NULL,
			'[]','{}','{}','{}','{"billing_type":" PER_SECOND ","unit_price":"1.25"}','{}',7,true,0);`)
	if err != nil {
		t.Fatal(err)
	}
	models := NewModelService(pool)
	model, err := models.GetFullByCode(ctx, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if model.PriceRule["billing_type"] != "per_second" || model.PriceRule["unit_price"] != 1.25 || models.EstimateCost(model, map[string]interface{}{"duration": 8}, 0, 0) != 10 {
		t.Fatalf("legacy quote and snapshot rule disagree: %#v", model.PriceRule)
	}
	snapshot := copyMap(model.PriceRule)
	if _, err := pool.Exec(ctx, `UPDATE models SET price_rule='{"billing_type":"per_second","unit_price":2}'`); err != nil {
		t.Fatal(err)
	}
	next, err := models.GetFullByCode(ctx, "legacy")
	if err != nil || next.PriceRule["unit_price"] != float64(2) || snapshot["unit_price"] != 1.25 {
		t.Fatalf("snapshot changed with subsequent pricing: old=%#v, err=%v", snapshot, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE models SET price_rule='{"billing_type":"per_request","unit_price":1,"unit_price_by_resolution":{"480p":"5.5","720p":"8.75"}}'`); err != nil {
		t.Fatal(err)
	}
	tiered, err := models.GetFullByCode(ctx, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	for resolution, price := range map[string]float64{"480p": 5.5, "720p": 8.75} {
		if got := models.EstimateCost(tiered, map[string]interface{}{"duration": 10, "resolution": resolution}, 0, 0); got != price {
			t.Fatalf("stored resolution quote=%v want=%v", got, price)
		}
	}
	snapshot = copyMap(tiered.PriceRule)
	if _, err := pool.Exec(ctx, `UPDATE models SET price_rule='{"billing_type":"per_request","unit_price":1,"unit_price_by_resolution":{"480p":99,"720p":199}}'`); err != nil {
		t.Fatal(err)
	}
	if _, err := models.GetFullByCode(ctx, "legacy"); err != nil {
		t.Fatal(err)
	}
	if snapshot["unit_price_by_resolution"].(map[string]interface{})["720p"] != 8.75 {
		t.Fatal("stored resolution snapshot changed with new prices")
	}
	for _, invalid := range []string{`{"billing_type":"per_second","unit_price":"NaN"}`, `{"billing_type":"per_image","unit_price":-1}`, `{"billing_type":"unsupported","unit_price":1}`, `{"billing_type":"per_request","unit_price":1,"unit_price_by_resolution":{"480p":-1}}`} {
		if _, err := pool.Exec(ctx, `UPDATE models SET price_rule=$1::jsonb`, invalid); err != nil {
			t.Fatal(err)
		}
		if _, err := models.GetFullByCode(ctx, "legacy"); err == nil {
			t.Fatalf("invalid stored multimedia price was accepted: %s", invalid)
		}
	}
}
