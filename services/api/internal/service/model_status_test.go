package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestModelStatusPreservesConfigurationAndRestoresPrimaryRoute(t *testing.T) {
	dsn := os.Getenv("TASK_READ_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TASK_READ_TEST_DATABASE_URL for model status regression")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf("model_status_test_%d", time.Now().UnixNano())
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
			id bigint PRIMARY KEY, code text DEFAULT 'legacy', display_name text DEFAULT 'Legacy',
			category text DEFAULT 'chat', icon_url text DEFAULT '', description text DEFAULT '',
			tags jsonb DEFAULT '[]', runtime_rule jsonb DEFAULT '{}', input_schema jsonb DEFAULT '{}',
			default_params jsonb DEFAULT '{}', price_rule jsonb DEFAULT '{}',
			new_api_extra_params jsonb DEFAULT '{"connection":{"api_key":"stored-secret"}}',
			is_enabled bool DEFAULT false, sort_order int DEFAULT 0, updated_at timestamptz DEFAULT now()
		);
		CREATE TABLE model_routes (
			id bigint PRIMARY KEY, model_id bigint REFERENCES models(id), priority int,
			is_enabled bool DEFAULT false, updated_at timestamptz DEFAULT now()
		);
		INSERT INTO models(id) VALUES (1), (2), (3);
		INSERT INTO model_routes(id,model_id,priority,is_enabled) VALUES
			(11,1,200,false), (12,1,100,false), (13,1,100,false),
			(21,2,100,false), (22,2,200,true);
	`); err != nil {
		t.Fatal(err)
	}
	svc := NewModelService(pool)
	for _, tc := range []struct {
		name    string
		id      int64
		enabled bool
		routes  string
	}{
		{"enable disabled legacy/default route", 1, true, "12"},
		{"disable model without disabling routes", 1, false, "12"},
		{"enable again", 1, true, "12"},
		{"preserve enabled alternate route", 2, true, "22"},
		{"enable model without routes", 3, true, ""},
		{"disable model without routes", 3, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model, err := svc.SetEnabled(ctx, tc.id, tc.enabled)
			if err != nil {
				t.Fatal(err)
			}
			if model.IsEnabled != tc.enabled {
				t.Fatalf("enabled=%v, want %v", model.IsEnabled, tc.enabled)
			}
			var enabledRoutes, extra, price string
			if err := pool.QueryRow(ctx, `SELECT COALESCE(string_agg(id::text, ',' ORDER BY id),'') FROM model_routes WHERE model_id=$1 AND is_enabled`, tc.id).Scan(&enabledRoutes); err != nil {
				t.Fatal(err)
			}
			if enabledRoutes != tc.routes {
				t.Fatalf("enabled routes=%q, want %q", enabledRoutes, tc.routes)
			}
			if err := pool.QueryRow(ctx, `SELECT new_api_extra_params #>> '{connection,api_key}', price_rule::text FROM models WHERE id=$1`, tc.id).Scan(&extra, &price); err != nil {
				t.Fatal(err)
			}
			if extra != "stored-secret" || price != "{}" {
				t.Fatalf("status toggle changed configuration: key=%q price=%q", extra, price)
			}
		})
	}
	if _, err := svc.SetEnabled(ctx, 99, true); err == nil || err.Error() != "模型不存在" {
		t.Fatalf("missing model error=%v", err)
	}
	// A failure while restoring the route must roll back the model status too.
	if _, err := pool.Exec(ctx, `UPDATE models SET is_enabled=false WHERE id=1;
		UPDATE model_routes SET is_enabled=false WHERE model_id=1;
		ALTER TABLE model_routes ADD CONSTRAINT reject_primary CHECK (id<>12 OR NOT is_enabled)`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetEnabled(ctx, 1, true); err == nil {
		t.Fatal("expected route constraint failure")
	}
	var enabled bool
	if err := pool.QueryRow(ctx, `SELECT is_enabled FROM models WHERE id=1`).Scan(&enabled); err != nil || enabled {
		t.Fatalf("failed activation changed model status: enabled=%v err=%v", enabled, err)
	}
}
