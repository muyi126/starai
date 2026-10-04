package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTerminalSettlementRemainder(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		actual, charged, want float64
	}{
		{"new charge", 12, 0, 12},
		{"partial workflow charge", 12, 5, 7},
		{"fully charged", 12, 12, 0},
		{"already overcharged", 12, 13, 0},
		{"six decimal precision", 0.3, 0.1, 0.2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := terminalSettlementRemainder(tc.actual, tc.charged); got != tc.want {
				t.Fatalf("remaining charge = %v, want %v", got, tc.want)
			}
		})
	}
}

// This test uses a private schema and must only run against a disposable PostgreSQL database.
func TestTerminalFreezeReconciliationChargesOnlyRemainingCost(t *testing.T) {
	dsn := os.Getenv("OPERATIONAL_GUARD_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set OPERATIONAL_GUARD_TEST_DATABASE_URL for isolated billing regression")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	schema := fmt.Sprintf("billing_guard_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
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
	if _, err = pool.Exec(ctx, `
		CREATE TABLE wallets(user_id bigint PRIMARY KEY, compute_balance numeric(18,6), frozen_compute numeric(18,6), updated_at timestamptz DEFAULT now());
		CREATE TABLE balance_freezes(id bigserial PRIMARY KEY, user_id bigint, amount numeric(18,6), ref_type text, ref_id text, status text, created_at timestamptz DEFAULT now(), released_at timestamptz);
		CREATE TABLE wallet_transactions(id bigserial PRIMARY KEY, user_id bigint, type text, direction text, amount numeric(18,6), balance_after numeric(18,6), ref_type text, ref_id text, remark text);
		CREATE TABLE tasks(id bigserial PRIMARY KEY, task_no text, status text, actual_cost numeric(18,6), type text);
		CREATE TABLE workflow_projects(id bigserial PRIMARY KEY, public_id text, status text, actual_cost numeric(18,6));
	`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO wallets(user_id,compute_balance,frozen_compute) VALUES (1,95,8),(2,95,8),(3,100,10);
		INSERT INTO workflow_projects(public_id,status,actual_cost) VALUES ('failed-partial','failed',12),('canceled-paid','canceled',5);
		INSERT INTO tasks(task_no,status,actual_cost,type) VALUES ('video-complete','succeeded',10,'video');
		INSERT INTO balance_freezes(user_id,amount,ref_type,ref_id,status) VALUES (1,8,'workflow','failed-partial','frozen'),(2,8,'workflow','canceled-paid','frozen'),(3,10,'task','video-complete','frozen');
		INSERT INTO wallet_transactions(user_id,type,direction,amount,balance_after,ref_type,ref_id) VALUES (1,'workflow_usage','out',5,95,'workflow','failed-partial'),(2,'workflow_usage','out',5,95,'workflow','canceled-paid');
	`); err != nil {
		t.Fatal(err)
	}
	svc := &OpsService{db: pool}
	if count, err := svc.settleTerminalStateFreezes(ctx); err != nil || count != 3 {
		t.Fatalf("reconciled %d freezes, err=%v", count, err)
	}
	for _, tc := range []struct {
		userID           int64
		balance, charged float64
		freezeStatus     string
	}{
		{1, 88, 12, "charged"},
		{2, 95, 5, "released"},
		{3, 90, 10, "charged"},
	} {
		var balance, frozen, charged float64
		var status string
		if err := pool.QueryRow(ctx, `SELECT compute_balance,frozen_compute FROM wallets WHERE user_id=$1`, tc.userID).Scan(&balance, &frozen); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT status FROM balance_freezes WHERE user_id=$1`, tc.userID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(amount),0) FROM wallet_transactions WHERE user_id=$1 AND direction='out'`, tc.userID).Scan(&charged); err != nil {
			t.Fatal(err)
		}
		if balance != tc.balance || frozen != 0 || charged != tc.charged || status != tc.freezeStatus {
			t.Fatalf("user %d: balance=%v frozen=%v charged=%v freeze=%s", tc.userID, balance, frozen, charged, status)
		}
	}
	if count, err := svc.settleTerminalStateFreezes(ctx); err != nil || count != 0 {
		t.Fatalf("second reconciliation processed %d freezes, err=%v", count, err)
	}
}
