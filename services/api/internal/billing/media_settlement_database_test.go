package billing

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMediaReservationSettlementIsAtomicAndSingleCharge(t *testing.T) {
	dsn := os.Getenv("MEDIA_BILLING_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set MEDIA_BILLING_TEST_DATABASE_URL for isolated PostgreSQL settlement checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("test database connection failed")
	}
	defer admin.Close()
	schema := fmt.Sprintf("media_billing_test_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal("test schema creation failed")
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error("test schema cleanup failed")
		}
	}()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("test database configuration failed")
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	// This fixture changes NUMERIC scale to check production storage behavior.
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("test database pool failed")
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `
		CREATE TABLE wallets (user_id BIGINT PRIMARY KEY, compute_balance NUMERIC NOT NULL, frozen_compute NUMERIC NOT NULL DEFAULT 0, updated_at TIMESTAMPTZ DEFAULT now());
		CREATE TABLE balance_freezes (id BIGSERIAL PRIMARY KEY, user_id BIGINT NOT NULL, amount NUMERIC NOT NULL, ref_type TEXT NOT NULL, ref_id TEXT NOT NULL, status TEXT NOT NULL, released_at TIMESTAMPTZ);
		CREATE TABLE wallet_transactions (id BIGSERIAL PRIMARY KEY, user_id BIGINT NOT NULL, type TEXT NOT NULL, direction TEXT NOT NULL, amount NUMERIC NOT NULL, balance_after NUMERIC NOT NULL, ref_type TEXT NOT NULL, ref_id TEXT NOT NULL, remark TEXT NOT NULL);
		CREATE TABLE media_tasks (task_no TEXT PRIMARY KEY, status TEXT NOT NULL);
		INSERT INTO wallets (user_id, compute_balance) VALUES (1,100);
		INSERT INTO media_tasks VALUES ('single','running'),('rollback','running'),('refund','failed');`)
	if err != nil {
		t.Fatal(err)
	}
	service := New(pool)
	if err := service.Freeze(ctx, 1, 10, "task", "single"); err != nil {
		t.Fatal(err)
	}
	// Repeating a reservation adjusts its amount, rather than adding another hold.
	if err := service.Freeze(ctx, 1, 12, "task", "single"); err != nil {
		t.Fatal(err)
	}
	finalize := func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE media_tasks SET status='succeeded' WHERE task_no='single' AND status='running'`)
		if err == nil && tag.RowsAffected() != 1 {
			return errors.New("task was already finalized")
		}
		return err
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results <- service.ChargeWithFinalize(ctx, 1, 10, 7, "task", "single", "video_usage", "test", finalize)
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrFreezeNotFound) {
			t.Fatalf("unexpected concurrent settlement error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful settlements = %d, want 1", successes)
	}
	balance, frozen, err := service.GetWallet(ctx, 1)
	if err != nil || balance != 93 || frozen != 0 {
		t.Fatalf("wallet after settlement = %v/%v, err = %v", balance, frozen, err)
	}
	var ledgerCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM wallet_transactions`).Scan(&ledgerCount); err != nil || ledgerCount != 1 {
		t.Fatalf("ledger count = %d, err = %v", ledgerCount, err)
	}
	if err := service.Freeze(ctx, 1, 5, "task", "rollback"); err != nil {
		t.Fatal(err)
	}
	if err := service.ChargeWithFinalize(ctx, 1, 5, 4, "task", "rollback", "audio_usage", "test", func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE media_tasks SET status='succeeded' WHERE task_no='rollback'`)
		if err != nil {
			return err
		}
		return errors.New("simulated finalize failure")
	}); err == nil {
		t.Fatal("settlement ignored finalization failure")
	}
	balance, frozen, err = service.GetWallet(ctx, 1)
	if err != nil || balance != 93 || frozen != 5 {
		t.Fatalf("failed finalization modified wallet = %v/%v, err = %v", balance, frozen, err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM media_tasks WHERE task_no='rollback'`).Scan(&status); err != nil || status != "running" {
		t.Fatalf("failed finalization modified task = %s, err = %v", status, err)
	}
	if err := service.Unfreeze(ctx, 1, 1, "task", "rollback"); err != nil {
		t.Fatal(err)
	}
	if err := service.Unfreeze(ctx, 1, 5, "task", "rollback"); err != nil {
		t.Fatal(err)
	}
	balance, frozen, err = service.GetWallet(ctx, 1)
	if err != nil || balance != 93 || frozen != 0 {
		t.Fatalf("idempotent full refund = %v/%v, err = %v", balance, frozen, err)
	}
	for _, invalid := range []float64{math.NaN(), math.Inf(1)} {
		if err := service.Freeze(ctx, 1, invalid, "task", "invalid"); !errors.Is(err, ErrInvalidAmount) {
			t.Fatalf("invalid reservation error = %v", err)
		}
		if err := service.Charge(ctx, 1, 1, invalid, "task", "single", "video_usage", "test"); !errors.Is(err, ErrInvalidAmount) {
			t.Fatalf("invalid settlement error = %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO media_tasks VALUES ('charge_refund_race','running')`); err != nil {
		t.Fatal(err)
	}
	if err := service.Freeze(ctx, 1, 9, "task", "charge_refund_race"); err != nil {
		t.Fatal(err)
	}
	finishRace := func(status string) func(pgx.Tx) error {
		return func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `UPDATE media_tasks SET status=$1 WHERE task_no='charge_refund_race' AND status='running'`, status)
			if err == nil && tag.RowsAffected() != 1 {
				return errors.New("competing task finalization won")
			}
			return err
		}
	}
	raceStart := make(chan struct{})
	raceResults := make(chan error, 2)
	go func() {
		<-raceStart
		raceResults <- service.ChargeWithFinalize(ctx, 1, 9, 7, "task", "charge_refund_race", "video_usage", "test", finishRace("succeeded"))
	}()
	go func() {
		<-raceStart
		raceResults <- service.UnfreezeWithFinalize(ctx, 1, 9, "task", "charge_refund_race", finishRace("cancelled"))
	}()
	close(raceStart)
	raceSuccess := 0
	for range 2 {
		if err := <-raceResults; err == nil {
			raceSuccess++
		}
	}
	if raceSuccess != 1 {
		t.Fatalf("charge/refund race finalized %d times, want 1", raceSuccess)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM media_tasks WHERE task_no='charge_refund_race'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM wallet_transactions WHERE ref_id='charge_refund_race'`).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	balance, frozen, err = service.GetWallet(ctx, 1)
	if err != nil || frozen != 0 || (status == "succeeded" && (balance != 86 || ledgerCount != 1)) || (status == "cancelled" && (balance != 93 || ledgerCount != 0)) {
		t.Fatalf("charge/refund outcome disagrees: status=%s wallet=%v/%v ledger=%d err=%v", status, balance, frozen, ledgerCount, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE wallets SET compute_balance='1e308'::numeric WHERE user_id=1`); err != nil {
		t.Fatal(err)
	}
	if err := service.Credit(ctx, 1, 1e308, "test", "test", "overflow", "test"); !errors.Is(err, ErrInvalidAmount) {
		t.Fatalf("overflowing credit error = %v", err)
	}
	balance, frozen, err = service.GetWallet(ctx, 1)
	if err != nil || math.IsInf(balance, 0) || math.IsNaN(balance) || frozen != 0 {
		t.Fatalf("overflow guard corrupted wallet = %v/%v, err = %v", balance, frozen, err)
	}
	// Match the production scale, rather than testing against unconstrained NUMERIC.
	_, err = pool.Exec(ctx, `
		UPDATE wallets SET compute_balance=93;
		ALTER TABLE wallets ALTER COLUMN compute_balance TYPE NUMERIC(18,6), ALTER COLUMN frozen_compute TYPE NUMERIC(18,6);
		ALTER TABLE balance_freezes ALTER COLUMN amount TYPE NUMERIC(18,6);
		ALTER TABLE wallet_transactions ALTER COLUMN amount TYPE NUMERIC(18,6), ALTER COLUMN balance_after TYPE NUMERIC(18,6);
		INSERT INTO media_tasks VALUES ('subunit','running');`)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Freeze(ctx, 1, 0.0000001, "task", "subunit"); err != nil {
		t.Fatal(err)
	}
	balance, frozen, err = service.GetWallet(ctx, 1)
	if err != nil || frozen != 0.000001 {
		t.Fatalf("positive subunit reservation was lost: wallet=%v/%v err=%v", balance, frozen, err)
	}
	if err := service.ChargeWithFinalize(ctx, 1, 0.0000001, 0.0000001, "task", "subunit", "image_usage", "test", func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE media_tasks SET status='succeeded' WHERE task_no='subunit'`)
		return err
	}); err != nil {
		t.Fatalf("subunit actual charge blocked task completion: %v", err)
	}
	balance, frozen, err = service.GetWallet(ctx, 1)
	if err != nil || balance != 93 || frozen != 0 {
		t.Fatalf("subunit zero-rounded settlement failed full release: %v/%v err=%v", balance, frozen, err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM wallet_transactions WHERE ref_id='subunit'`).Scan(&ledgerCount); err != nil || ledgerCount != 0 {
		t.Fatalf("zero-rounded amount emitted a debit: count=%d err=%v", ledgerCount, err)
	}
	if err := service.Freeze(ctx, 1, math.SmallestNonzeroFloat64, "task", "smallest"); err != nil {
		t.Fatal(err)
	}
	_, frozen, err = service.GetWallet(ctx, 1)
	if err != nil || frozen != 0.000001 {
		t.Fatalf("smallest positive amount was swallowed by epsilon: frozen=%v err=%v", frozen, err)
	}
	if err := service.Unfreeze(ctx, 1, 0, "task", "smallest"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE wallets SET compute_balance=0.3 WHERE user_id=1`); err != nil {
		t.Fatal(err)
	}
	if err := service.Freeze(ctx, 1, 0.1, "task", "decimal_a"); err != nil {
		t.Fatal(err)
	}
	if err := service.Freeze(ctx, 1, 0.2, "task", "decimal_b"); err != nil {
		t.Fatalf("exactly funded decimal reservation was rejected by binary subtraction: %v", err)
	}
	_, frozen, err = service.GetWallet(ctx, 1)
	if err != nil || frozen != 0.3 {
		t.Fatalf("decimal reservations do not match stored precision: frozen=%v err=%v", frozen, err)
	}
}
