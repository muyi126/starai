package service

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type dashboardQueryCounter struct{ count int }

func (q *dashboardQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	q.count++
	return ctx
}
func (*dashboardQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestDashboardDatabase(t *testing.T) {
	dsn := os.Getenv("PERFORMANCE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set PERFORMANCE_TEST_DATABASE_URL for isolated dashboard regression")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	counter := &dashboardQueryCounter{}
	cfg.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `
		CREATE TEMP TABLE users(created_at timestamptz,referrer_id bigint);
		CREATE TEMP TABLE tasks(created_at timestamptz,status text);
		CREATE TEMP TABLE models(is_enabled boolean);
		CREATE TEMP TABLE orders(status text,amount numeric);
		CREATE TEMP TABLE wallet_transactions(direction text,type text,amount numeric,created_at timestamptz);
		CREATE TEMP TABLE api_tokens(status text);
		CREATE TEMP TABLE ai_call_logs(created_at timestamptz,cost numeric);
		CREATE TEMP TABLE recharge_cards(status text,value numeric);
		CREATE TEMP TABLE wallets(compute_balance numeric);
		CREATE TEMP TABLE gallery_items(status text);
		CREATE TEMP TABLE announcements(is_published boolean);
		CREATE TEMP TABLE referral_rewards(reward_account text,amount numeric);`)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewAdminService(pool, nil, "")
	counter.count = 0
	stats, err := svc.Dashboard(ctx)
	if err != nil || !reflect.DeepEqual(stats, &DashboardStats{}) || counter.count != 1 {
		t.Fatalf("empty dashboard: %+v err=%v queries=%d", stats, err, counter.count)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO users VALUES(now(),NULL),(now(),1),(CURRENT_DATE-INTERVAL '1 day',1),(CURRENT_DATE-INTERVAL '1 day',2);
		INSERT INTO tasks VALUES(now(),'succeeded'),(now(),'pending'),(CURRENT_DATE-INTERVAL '1 day','failed');
		INSERT INTO models VALUES(true),(false);
		INSERT INTO orders VALUES('paid',10),('unpaid',100);
		INSERT INTO wallet_transactions VALUES('in','card_recharge',3,now()),('in','other',100,now()),('out','charge',2,now()),('out','charge',5,CURRENT_DATE-INTERVAL '1 day');
		INSERT INTO api_tokens VALUES('active'),('revoked');
		INSERT INTO ai_call_logs VALUES(now(),0.5),(CURRENT_DATE-INTERVAL '1 day',1.5);
		INSERT INTO recharge_cards VALUES('unused',10),('used',20),('disabled',30);
		INSERT INTO wallets VALUES(10.25),(-0.25);
		INSERT INTO gallery_items VALUES('approved'),('pending');
		INSERT INTO announcements VALUES(true),(false);
		INSERT INTO referral_rewards VALUES('compute',1.25),('cash',2.5),('other',100);`)
	if err != nil {
		t.Fatal(err)
	}
	counter.count = 0
	stats, err = svc.Dashboard(ctx)
	want := &DashboardStats{TotalUsers: 4, NewUsersToday: 2, ReferredUsers: 3, ActiveReferrers: 2,
		TotalTasks: 3, TasksToday: 2, SucceededTasks: 1, FailedTasks: 1, ActiveModels: 1,
		TotalRevenue: 13, OnlineRevenue: 10, CardRechargeAmount: 3, TotalConsumption: 7, ConsumptionToday: 2,
		ApiTokens: 1, ApiCalls: 2, ApiCallsToday: 1, ApiCost: 2, AvailableCards: 1, UsedCards: 1,
		TotalCardFaceValue: 60, WalletBalanceTotal: 10, PublishedWorks: 1, PublishedAnnouncements: 1,
		ReferralRewardCompute: 1.25, ReferralRewardCash: 2.5}
	if err != nil || !reflect.DeepEqual(stats, want) || counter.count != 1 {
		t.Fatalf("dashboard: %+v want=%+v err=%v queries=%d", stats, want, err, counter.count)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if stats, err := svc.Dashboard(cancelled); err == nil || stats != nil {
		t.Fatalf("query error returned misleading statistics: %+v %v", stats, err)
	}
	t.Run("public config excludes large private fields at SQL boundary", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `CREATE TEMP TABLE system_configs(key text PRIMARY KEY,value jsonb);
			INSERT INTO system_configs VALUES('site_name','"Test"'),('web_search_unit_price','0.25'),
			('web_search_api_key','"private fixture"'),('ui_translation_overrides','[]');`); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO system_configs VALUES('content_safety_sensitive_words',to_jsonb($1::text))`, strings.Repeat("fixture", 150_000)); err != nil {
			t.Fatal(err)
		}
		cfg, err := svc.GetPublicSystemConfigs(ctx)
		if err != nil || len(cfg) != 2 || cfg["site_name"] != "Test" || cfg["web_search_unit_price"] != 0.25 {
			t.Fatalf("public config fetched unrelated fields: %#v err=%v", cfg, err)
		}
	})
}
