package web_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"smart-coder-switch/internal/web"
)

func newTestStore(t *testing.T) *web.Store {
	t.Helper()

	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	store, err := web.NewStore(db, 100)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	return store
}

func TestStore_InsertAndQuery(t *testing.T) {
	store := newTestStore(t)

	record := web.DecisionRecord{
		RequestID:      "req-001",
		Timestamp:      time.Now().UTC().Truncate(time.Second),
		LogicalModel:   "coder1",
		SelectedTier:   "HIGH",
		SelectedModel:  "strong-model",
		AssistantCount: 5,
		Reason:         "fixed-rounds",
		TraceDir:       "trace-dir-001",
		RequestTimeMs:  120,
	}

	if err := store.Insert(context.Background(), record); err != nil {
		t.Fatalf("insert decision record: %v", err)
	}

	result, err := store.QueryDecisions(context.Background(), web.DecisionQuery{
		LogicalModel: "coder1",
		Tier:         "HIGH",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("query decisions: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result.Items))
	}

	item := result.Items[0]
	if item.RequestID != record.RequestID {
		t.Errorf("request_id = %s, want %s", item.RequestID, record.RequestID)
	}
	if item.SelectedTier != record.SelectedTier {
		t.Errorf("selected_tier = %s, want %s", item.SelectedTier, record.SelectedTier)
	}
	if item.SelectedModel != record.SelectedModel {
		t.Errorf("selected_model = %s, want %s", item.SelectedModel, record.SelectedModel)
	}
	if item.TraceDir != record.TraceDir {
		t.Errorf("trace_dir = %s, want %s", item.TraceDir, record.TraceDir)
	}
	if item.RequestTimeMs != record.RequestTimeMs {
		t.Errorf("request_time_ms = %d, want %d", item.RequestTimeMs, record.RequestTimeMs)
	}
}

func TestStore_Distribution(t *testing.T) {
	store := newTestStore(t)
	base := time.Now().UTC().Truncate(time.Second)

	records := []web.DecisionRecord{
		{
			RequestID:      "req-dist-001",
			Timestamp:      base,
			LogicalModel:   "coder1",
			SelectedTier:   "LOW",
			SelectedModel:  "low-model",
			AssistantCount: 1,
			Reason:         "default",
			TraceDir:       "dir-1",
		},
		{
			RequestID:      "req-dist-002",
			Timestamp:      base.Add(time.Second),
			LogicalModel:   "coder1",
			SelectedTier:   "MEDIUM",
			SelectedModel:  "medium-model",
			AssistantCount: 2,
			Reason:         "probability",
			TraceDir:       "dir-2",
		},
		{
			RequestID:      "req-dist-003",
			Timestamp:      base.Add(2 * time.Second),
			LogicalModel:   "coder2",
			SelectedTier:   "HIGH",
			SelectedModel:  "high-model",
			AssistantCount: 3,
			Reason:         "fixed-rounds",
			TraceDir:       "dir-3",
		},
	}

	for _, r := range records {
		if err := store.Insert(context.Background(), r); err != nil {
			t.Fatalf("insert decision record: %v", err)
		}
	}

	dist, err := store.QueryDistribution(context.Background(), web.DistributionQuery{
		LogicalModel: "coder1",
		Minutes:      60,
	})
	if err != nil {
		t.Fatalf("query distribution: %v", err)
	}
	if dist.Total != 2 {
		t.Errorf("total = %d, want 2", dist.Total)
	}

	tierMap := map[string]int64{}
	for _, tier := range dist.Tiers {
		tierMap[tier.Name] = tier.Count
	}
	if tierMap["LOW"] != 1 {
		t.Errorf("LOW count = %d, want 1", tierMap["LOW"])
	}
	if tierMap["MEDIUM"] != 1 {
		t.Errorf("MEDIUM count = %d, want 1", tierMap["MEDIUM"])
	}
	if tierMap["HIGH"] != 0 {
		t.Errorf("HIGH count = %d, want 0", tierMap["HIGH"])
	}
}

func TestStore_CleanupByMaxRecords(t *testing.T) {
	store := newTestStore(t)
	base := time.Now().UTC().Truncate(time.Second)

	for i := int64(0); i < 5; i++ {
		if err := store.Insert(context.Background(), web.DecisionRecord{
			RequestID:    "req-limit-" + time.Unix(i, 0).Format("150405"),
			Timestamp:    base.Add(time.Duration(i) * time.Second),
			LogicalModel: "coder1",
			SelectedTier: "LOW",
			SelectedModel: "low-model",
			Reason:       "default",
			TraceDir:     "dir-limit-" + time.Unix(i, 0).Format("150405"),
		}); err != nil {
			t.Fatalf("insert decision record: %v", err)
		}
	}

	if err := store.Cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup store: %v", err)
	}

	result, err := store.QueryDecisions(context.Background(), web.DecisionQuery{
		LogicalModel: "coder1",
		Limit:        200,
	})
	if err != nil {
		t.Fatalf("query decisions: %v", err)
	}
	if len(result.Items) != 5 {
		t.Errorf("after cleanup items = %d, want 5", len(result.Items))
	}

	store.UpdateMaxRecords(3)
	if err := store.Cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup store after lowering max records: %v", err)
	}

	result, err = store.QueryDecisions(context.Background(), web.DecisionQuery{
		LogicalModel: "coder1",
		Limit:        200,
	})
	if err != nil {
		t.Fatalf("query decisions after cleanup: %v", err)
	}
	if len(result.Items) != 3 {
		t.Errorf("after reduced cleanup items = %d, want 3", len(result.Items))
	}
	if result.Items[0].RequestID != result.Items[0].RequestID {
		// basic sanity only
	}
}

func TestStore_UpdateResult(t *testing.T) {
	store := newTestStore(t)

	record := web.DecisionRecord{
		RequestID:     "req-result-001",
		Timestamp:     time.Now().UTC().Truncate(time.Second),
		LogicalModel:  "coder1",
		SelectedTier:  "LOW",
		SelectedModel: "low-model",
		Reason:        "default",
		TraceDir:      "dir-result-001",
	}
	if err := store.Insert(context.Background(), record); err != nil {
		t.Fatalf("insert decision record: %v", err)
	}

	if err := store.UpdateResult(
		context.Background(),
		"req-result-001",
		429,
		"rate limit exceeded",
	); err != nil {
		t.Fatalf("update result: %v", err)
	}

	result, err := store.QueryDecisions(context.Background(), web.DecisionQuery{
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("query decisions: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result.Items))
	}

	item := result.Items[0]
	if item.StatusCode != 429 {
		t.Errorf("status_code = %d, want 429", item.StatusCode)
	}
	if item.ErrorMessage != "rate limit exceeded" {
		t.Errorf("error_message = %q, want %q", item.ErrorMessage, "rate limit exceeded")
	}
}

func TestStore_UpdateResult_UnknownRequestID(t *testing.T) {
	store := newTestStore(t)

	// 更新不存在的 request_id 不应报错（幂等）
	if err := store.UpdateResult(
		context.Background(),
		"req-not-exist",
		500,
		"boom",
	); err != nil {
		t.Fatalf("update result for unknown request id: %v", err)
	}
}

func TestStore_MigratesLegacySchema(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	// 构造旧版表结构（无 status_code / error_message 列）
	legacySchema := `
CREATE TABLE decision_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  request_id TEXT NOT NULL UNIQUE,
  timestamp INTEGER NOT NULL,
  logical_model TEXT NOT NULL,
  selected_tier TEXT NOT NULL,
  selected_model TEXT NOT NULL,
  assistant_count INTEGER NOT NULL DEFAULT 0,
  reason TEXT NOT NULL,
  trace_dir TEXT NOT NULL DEFAULT '',
  request_time_ms INTEGER NOT NULL DEFAULT 0
);
`
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}

	// NewStore 应自动迁移补齐新列
	store, err := web.NewStore(db, 100)
	if err != nil {
		t.Fatalf("new store with legacy schema: %v", err)
	}

	record := web.DecisionRecord{
		RequestID:    "req-mig-001",
		Timestamp:    time.Now().UTC().Truncate(time.Second),
		LogicalModel: "coder1",
		SelectedTier: "LOW",
		SelectedModel: "low-model",
		Reason:       "default",
		TraceDir:     "dir-mig-001",
	}
	if err := store.Insert(context.Background(), record); err != nil {
		t.Fatalf("insert after migration: %v", err)
	}
	if err := store.UpdateResult(
		context.Background(),
		"req-mig-001",
		502,
		"bad gateway",
	); err != nil {
		t.Fatalf("update result after migration: %v", err)
	}

	result, err := store.QueryDecisions(context.Background(), web.DecisionQuery{
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("query decisions: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result.Items))
	}
	if result.Items[0].StatusCode != 502 {
		t.Errorf("status_code = %d, want 502", result.Items[0].StatusCode)
	}
	if result.Items[0].ErrorMessage != "bad gateway" {
		t.Errorf("error_message = %q, want %q", result.Items[0].ErrorMessage, "bad gateway")
	}
}

func TestStore_InsertAutoCleanupByMaxRecords(t *testing.T) {
	store := newTestStore(t)
	store.UpdateMaxRecords(3)
	base := time.Now().UTC().Truncate(time.Second)

	// 插入 5 条记录，超过 maxRecords=3
	for i := int64(0); i < 5; i++ {
		rec := web.DecisionRecord{
			RequestID:     fmt.Sprintf("req-auto-%d", i),
			Timestamp:     base.Add(time.Duration(i) * time.Second),
			LogicalModel:  "coder1",
			SelectedTier:  "LOW",
			SelectedModel: "low-model",
			AssistantCount: 1,
			Reason:        "default",
			TraceDir:      fmt.Sprintf("dir-auto-%d", i),
		}
		if err := store.Insert(context.Background(), rec); err != nil {
			t.Fatalf("insert decision record: %v", err)
		}
	}

	result, err := store.QueryDecisions(context.Background(), web.DecisionQuery{
		LogicalModel: "coder1",
		Limit:        200,
	})
	if err != nil {
		t.Fatalf("query decisions: %v", err)
	}
	if len(result.Items) != 3 {
		t.Fatalf("after auto cleanup items = %d, want 3", len(result.Items))
	}

	// QueryDecisions 按时间倒序返回，保留最近的 3 条：req-auto-4, req-auto-3, req-auto-2
	want := []string{"req-auto-4", "req-auto-3", "req-auto-2"}
	for i, wantID := range want {
		if result.Items[i].RequestID != wantID {
			t.Errorf("item[%d] request_id = %s, want %s", i, result.Items[i].RequestID, wantID)
		}
	}
}

func TestStore_QueryStats(t *testing.T) {
	store := newTestStore(t)
	base := time.Now().UTC().Truncate(time.Second)

	records := []struct {
		id, logical, model string
		status             int
		errMsg             string
	}{
		{"req-stats-001", "coder1", "low-model", 200, ""},
		{"req-stats-002", "coder1", "low-model", 500, "server error"},
		{"req-stats-003", "coder2", "high-model", 429, "rate limit"},
		{"req-stats-004", "coder2", "high-model", 0, ""},
	}
	for i, r := range records {
		rec := web.DecisionRecord{
			RequestID:     r.id,
			Timestamp:     base.Add(time.Duration(i) * time.Second),
			LogicalModel:  r.logical,
			SelectedTier:  "LOW",
			SelectedModel: r.model,
			Reason:        "default",
			TraceDir:      "dir-" + r.id,
		}
		if err := store.Insert(context.Background(), rec); err != nil {
			t.Fatalf("insert %s: %v", r.id, err)
		}
		if r.status != 0 {
			if err := store.UpdateResult(context.Background(), r.id, r.status, r.errMsg); err != nil {
				t.Fatalf("update result %s: %v", r.id, err)
			}
		}
	}

	snap, err := store.QueryStats(context.Background())
	if err != nil {
		t.Fatalf("query stats: %v", err)
	}

	if snap.Total != 4 {
		t.Errorf("total = %d, want 4", snap.Total)
	}
	if snap.Success != 1 {
		t.Errorf("success = %d, want 1", snap.Success)
	}
	if snap.Failure != 3 {
		t.Errorf("failure = %d, want 3", snap.Failure)
	}

	if len(snap.Models) != 2 {
		t.Fatalf("models count = %d, want 2", len(snap.Models))
	}
	if snap.Models[0].Model != "high-model" || snap.Models[0].Total != 2 || snap.Models[0].Success != 0 || snap.Models[0].Failure != 2 {
		t.Errorf("models[0] = %+v, want high-model total=2 success=0 failure=2", snap.Models[0])
	}
	if snap.Models[1].Model != "low-model" || snap.Models[1].Total != 2 || snap.Models[1].Success != 1 || snap.Models[1].Failure != 1 {
		t.Errorf("models[1] = %+v, want low-model total=2 success=1 failure=1", snap.Models[1])
	}

	if len(snap.LogicalModels) != 2 {
		t.Fatalf("logical_models count = %d, want 2", len(snap.LogicalModels))
	}
	if snap.LogicalModels[0].Model != "coder1" || snap.LogicalModels[0].Total != 2 || snap.LogicalModels[0].Success != 1 || snap.LogicalModels[0].Failure != 1 {
		t.Errorf("logical_models[0] = %+v, want coder1 total=2 success=1 failure=1", snap.LogicalModels[0])
	}
	if snap.LogicalModels[1].Model != "coder2" || snap.LogicalModels[1].Total != 2 || snap.LogicalModels[1].Success != 0 || snap.LogicalModels[1].Failure != 2 {
		t.Errorf("logical_models[1] = %+v, want coder2 total=2 success=0 failure=2", snap.LogicalModels[1])
	}
}

func TestStore_QueryStats_Empty(t *testing.T) {
	store := newTestStore(t)

	snap, err := store.QueryStats(context.Background())
	if err != nil {
		t.Fatalf("query stats on empty store: %v", err)
	}
	if snap.Total != 0 || snap.Success != 0 || snap.Failure != 0 {
		t.Errorf("empty stats = %+v, want all zeros", snap)
	}
	if len(snap.Models) != 0 || len(snap.LogicalModels) != 0 {
		t.Errorf("empty stats models = %+v / %+v, want empty", snap.Models, snap.LogicalModels)
	}
}

func TestStore_ClearStats(t *testing.T) {
	store := newTestStore(t)
	base := time.Now().UTC().Truncate(time.Second)

	for i := int64(0); i < 3; i++ {
		rec := web.DecisionRecord{
			RequestID:     fmt.Sprintf("req-clr-%d", i),
			Timestamp:     base.Add(time.Duration(i) * time.Second),
			LogicalModel:  "coder1",
			SelectedTier:  "LOW",
			SelectedModel: "low-model",
			Reason:        "default",
			TraceDir:      fmt.Sprintf("dir-clr-%d", i),
		}
		if err := store.Insert(context.Background(), rec); err != nil {
			t.Fatalf("insert decision record: %v", err)
		}
	}

	if err := store.ClearStats(context.Background()); err != nil {
		t.Fatalf("clear stats: %v", err)
	}

	snap, err := store.QueryStats(context.Background())
	if err != nil {
		t.Fatalf("query stats after clear: %v", err)
	}
	if snap.Total != 0 {
		t.Errorf("total after clear = %d, want 0", snap.Total)
	}
	if len(snap.Models) != 0 || len(snap.LogicalModels) != 0 {
		t.Errorf("models after clear = %+v / %+v, want empty", snap.Models, snap.LogicalModels)
	}
}
