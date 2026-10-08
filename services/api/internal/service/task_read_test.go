package service

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTaskProgressPreservesFallbacks(t *testing.T) {
	for _, tc := range []struct {
		status, raw string
		cancelled   bool
		want        int
	}{
		{"succeeded", "", false, 100}, {"failed", "20", false, 100},
		{"cancelled", "20", true, 100}, {"cancelled", "20", false, 20},
		{"canceled", "", true, 8}, {"pending", "", true, 8},
		{"running", "broken", true, 25}, {"processing", "0", false, 25},
		{"in_progress", "-1", false, 25}, {"running", "101", true, 99},
		{"running", " 42 ", false, 42}, {"running", "2147483648", false, 25},
		{"running", "2.5", false, 25},
	} {
		if got := taskProgress(tc.status, tc.raw, tc.cancelled); got != tc.want {
			t.Errorf("taskProgress(%q, %q, %v)=%d, want %d", tc.status, tc.raw, tc.cancelled, got, tc.want)
		}
	}
}

func TestChineseTranslationSkipsDatabase(t *testing.T) {
	service := NewContentI18nService(nil)
	for _, locale := range []string{"zh-CN", "zh-TW", ""} {
		if err := service.ApplyBatch(context.Background(), "model", locale, map[string]interface{}{"model": &ModelDTO{}}); err != nil {
			t.Fatal(err)
		}
	}
}

// Opt-in: only connection-local temporary tables are created, even when the
// supplied database contains other application tables.
func TestTaskReadDatabase(t *testing.T) {
	dsn := os.Getenv("TASK_READ_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TASK_READ_TEST_DATABASE_URL for isolated task read regression")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, `
		CREATE TEMP TABLE models (id bigint PRIMARY KEY, code text, display_name text);
		CREATE TEMP TABLE users (id bigint PRIMARY KEY, nickname text);
		CREATE TEMP TABLE auth_identities (id bigint PRIMARY KEY, user_id bigint, provider text, identifier text);
		CREATE TEMP TABLE tasks (
		  id bigint PRIMARY KEY, task_no text UNIQUE, user_id bigint, status text,
		  output jsonb DEFAULT '{}', error_message text, upstream_task_id text,
		  type text DEFAULT 'image', model_id bigint, input jsonb DEFAULT '{}',
		  estimated_cost numeric DEFAULT 1, actual_cost numeric DEFAULT 0,
		  error_code text, created_at timestamptz DEFAULT now(), finished_at timestamptz);
		CREATE TEMP TABLE task_events (
		  id bigserial PRIMARY KEY, task_id bigint, event_type text,
		  payload jsonb, created_at timestamptz DEFAULT now());
		INSERT INTO tasks(id,task_no,user_id,status) VALUES
		  (1,'running',1,'running'),(2,'success',1,'succeeded'),(3,'bad-progress',1,'running'),
		  (4,'private',2,'running'),(5,'pending',1,'pending'),(6,'cancelled',1,'cancelled');
		INSERT INTO users VALUES (1,'Fixture'),(2,'Other fixture');
		INSERT INTO models VALUES (1,'seedance25','章鱼哥 Seedance 2.5');
		UPDATE tasks SET model_id=1 WHERE task_no='running';
		INSERT INTO task_events(task_id,event_type,payload) VALUES
		  (1,'progress','{"progress":12,"status":"old"}'),
		  (1,'progress','{"progress":67,"status":"generating"}'),
		  (1,'other','{"progress":99}'),(3,'progress','{"progress":"invalid"}'),
		  (4,'progress','{"progress":88}'),(6,'progress','{"progress":42}');`)
	if err != nil {
		t.Fatal(err)
	}
	tasks := &TaskService{db: pool}
	adminItems, total, err := tasks.ListAdmin(ctx, 1, 20, "running")
	if err != nil || total != 3 || len(adminItems) != 3 {
		t.Fatalf("admin list: count=%d total=%d err=%v", len(adminItems), total, err)
	}
	for _, task := range adminItems {
		if task.TaskNo == "running" && (task.ModelName == nil || *task.ModelName != "章鱼哥 Seedance 2.5" || task.ModelCode == nil || *task.ModelCode != "seedance25") {
			t.Fatalf("admin model display: %+v", task)
		}
	}
	before := pool.Stat().AcquireCount()
	item, err := tasks.Get(ctx, 1, "running")
	if err != nil || item.Progress != 67 || item.UpstreamStatus != "generating" {
		t.Fatalf("task detail: %+v, %v", item, err)
	}
	if count := pool.Stat().AcquireCount() - before; count != 1 {
		t.Fatalf("task detail used %d queries, want 1", count)
	}
	if _, err := tasks.Get(ctx, 1, "private"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-user task should be inaccessible: %v", err)
	}
	adminTask, err := tasks.GetAdmin(ctx, "private")
	if err != nil || adminTask.Progress != 88 {
		t.Fatalf("admin task detail: %+v, %v", adminTask, err)
	}
	for code, want := range map[string]int{"success": 100, "bad-progress": 25, "pending": 8, "cancelled": 100} {
		item, err := tasks.Get(ctx, 1, code)
		if err != nil || item.Progress != want {
			t.Fatalf("%s detail: %+v, %v", code, item, err)
		}
	}
	before = pool.Stat().AcquireCount()
	batch, err := tasks.GetMany(ctx, 1, []string{"success", "running", "private", "running", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if count := pool.Stat().AcquireCount() - before; count != 1 {
		t.Fatalf("task batch used %d queries, want 1", count)
	}
	if len(batch) != 2 || batch[0].TaskNo != "success" || batch[1].TaskNo != "running" || batch[1].Progress != 67 {
		t.Fatalf("task batch changed order, deduplication or ownership: %+v", batch)
	}
	agents := &AgentService{db: pool}
	input := []AgentMediaTaskDTO{
		{TaskNo: "running", Type: "video"}, {TaskNo: "success"}, {TaskNo: "running"},
		{TaskNo: "bad-progress"}, {TaskNo: "private", Status: "snapshot"},
		{TaskNo: "missing", Status: "snapshot"}, {TaskNo: "cancelled"},
	}
	before = pool.Stat().AcquireCount()
	result := agents.refreshMediaTasks(ctx, 1, input)
	if count := pool.Stat().AcquireCount() - before; count != 1 {
		t.Fatalf("media batch used %d queries, want 1", count)
	}
	if len(result) != 7 || result[0].Progress != 67 || result[0].Type != "video" ||
		result[1].Progress != 100 || result[2].Progress != 67 || result[3].Progress != 25 ||
		result[4].Status != "snapshot" || result[5].Status != "snapshot" || result[6].Progress != 42 {
		t.Fatalf("media batch changed order, scope or progress semantics: %+v", result)
	}
}
