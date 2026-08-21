package web

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"smart-coder-switch/internal/stats"
)

const (
	decisionSchema = `
CREATE TABLE IF NOT EXISTS decision_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  request_id TEXT NOT NULL UNIQUE,
  timestamp INTEGER NOT NULL,
  logical_model TEXT NOT NULL,
  selected_tier TEXT NOT NULL,
  selected_model TEXT NOT NULL,
  assistant_count INTEGER NOT NULL DEFAULT 0,
  reason TEXT NOT NULL,
  trace_dir TEXT NOT NULL DEFAULT '',
  request_time_ms INTEGER NOT NULL DEFAULT 0,
  status_code INTEGER NOT NULL DEFAULT 0,
  error_message TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_decision_logs_timestamp ON decision_logs(timestamp);
CREATE INDEX IF NOT EXISTS idx_decision_logs_logical_model ON decision_logs(logical_model);
CREATE INDEX IF NOT EXISTS idx_decision_logs_selected_tier ON decision_logs(selected_tier);
`
	decisionInsertSQL = `
INSERT INTO decision_logs (
  request_id,
  timestamp,
  logical_model,
  selected_tier,
  selected_model,
  assistant_count,
  reason,
  trace_dir,
  request_time_ms,
  status_code,
  error_message
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
`
	// decisionResultUpdateSQL 按 request_id 回写上游转发结果。
	decisionResultUpdateSQL = `
UPDATE decision_logs
SET status_code = ?, error_message = ?
WHERE request_id = ?;
`
	// decisionLegacyColumnSQL 查询 decision_logs 现有列，用于旧库迁移。
	decisionLegacyColumnSQL = `
PRAGMA table_info(decision_logs);
`
	decisionCountByMaxRecordsSQL = `
DELETE FROM decision_logs
WHERE id IN (
  SELECT id FROM decision_logs
  ORDER BY timestamp DESC, id DESC
  LIMIT -1 OFFSET ?
);
`
	// decisionStatsTotalSQL 统计总调用量、成功量（2xx）与失败量（其余）。
	decisionStatsTotalSQL = `
SELECT
  COUNT(*) AS total,
  COALESCE(SUM(CASE WHEN status_code >= 200 AND status_code < 300 THEN 1 ELSE 0 END), 0) AS success,
  COALESCE(SUM(CASE WHEN status_code < 200 OR status_code >= 300 THEN 1 ELSE 0 END), 0) AS failure
FROM decision_logs;
`
	// decisionStatsByModelSQL 按模型分组统计。
	// 第一个 %s 为分组列名，第二个 %s 为排序键列名。
	decisionStatsByModelSQL = `
SELECT %s AS model,
  COUNT(*) AS total,
  COALESCE(SUM(CASE WHEN status_code >= 200 AND status_code < 300 THEN 1 ELSE 0 END), 0) AS success,
  COALESCE(SUM(CASE WHEN status_code < 200 OR status_code >= 300 THEN 1 ELSE 0 END), 0) AS failure
FROM decision_logs
GROUP BY %s
ORDER BY model ASC;
`
	// decisionClearStatsSQL 清空决策记录（同时清空统计口径来源）。
	decisionClearStatsSQL = `DELETE FROM decision_logs;`
)

type Store struct {
	db         *sql.DB
	maxRecords int
}

type DecisionRecord struct {
	RequestID      string
	Timestamp      time.Time
	LogicalModel   string
	SelectedTier   string
	SelectedModel  string
	AssistantCount int64
	Reason         string
	TraceDir       string
	RequestTimeMs  int64
	StatusCode     int
	ErrorMessage   string
}

type DecisionItem struct {
	RequestID      string `json:"request_id"`
	Timestamp      int64  `json:"timestamp"`
	LogicalModel   string `json:"logical_model"`
	SelectedTier   string `json:"selected_tier"`
	SelectedModel  string `json:"selected_model"`
	AssistantCount int64  `json:"assistant_count"`
	Reason         string `json:"reason"`
	TraceDir       string `json:"trace_dir"`
	RequestTimeMs  int64  `json:"request_time_ms"`
	StatusCode     int    `json:"status_code"`
	ErrorMessage   string `json:"error_message"`
}

type DecisionQuery struct {
	LogicalModel string
	Tier         string
	Query        string
	Limit        int
	Before       *time.Time
}

type DecisionResult struct {
	Items   []DecisionItem `json:"items"`
	HasMore bool           `json:"has_more"`
}

type DistributionQuery struct {
	LogicalModel string
	Minutes      int
}

type DistributionTier struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
	Ratio float64 `json:"ratio"`
}

type DistributionResult struct {
	Total  int64            `json:"total"`
	Tiers  []DistributionTier `json:"tiers"`
}

func NewStore(db *sql.DB, maxRecords int) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is required")
	}
	if maxRecords <= 0 {
		maxRecords = 100
	}

	if _, err := db.ExecContext(context.Background(), decisionSchema); err != nil {
		return nil, fmt.Errorf("init decision_logs schema: %w", err)
	}

	// 旧库迁移：检测并补齐 status_code / error_message 列
	if err := migrateLegacyColumns(db); err != nil {
		return nil, fmt.Errorf("migrate legacy decision_logs columns: %w", err)
	}

	return &Store{
		db:         db,
		maxRecords: maxRecords,
	}, nil
}

// migrateLegacyColumns 检测 decision_logs 表是否缺少 status_code / error_message 列，
// 如缺少则自动补齐（支持从旧版本平滑升级）。
func migrateLegacyColumns(db *sql.DB) error {
	rows, err := db.QueryContext(context.Background(), decisionLegacyColumnSQL)
	if err != nil {
		return fmt.Errorf("query table info: %w", err)
	}
	defer rows.Close()

	hasStatusCode := false
	hasErrorMessage := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("scan table info row: %w", err)
		}
		if name == "status_code" {
			hasStatusCode = true
		}
		if name == "error_message" {
			hasErrorMessage = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate table info rows: %w", err)
	}

	if !hasStatusCode {
		if _, err := db.ExecContext(
			context.Background(),
			`ALTER TABLE decision_logs ADD COLUMN status_code INTEGER NOT NULL DEFAULT 0;`,
		); err != nil {
			return fmt.Errorf("add status_code column: %w", err)
		}
	}
	if !hasErrorMessage {
		if _, err := db.ExecContext(
			context.Background(),
			`ALTER TABLE decision_logs ADD COLUMN error_message TEXT NOT NULL DEFAULT '';`,
		); err != nil {
			return fmt.Errorf("add error_message column: %w", err)
		}
	}
	return nil
}

func (s *Store) UpdateMaxRecords(maxRecords int) {
	if maxRecords <= 0 {
		maxRecords = 100
	}
	s.maxRecords = maxRecords
}

func (s *Store) Insert(ctx context.Context, record DecisionRecord) error {
	if record.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}

	_, err := s.db.ExecContext(
		ctx,
		decisionInsertSQL,
		record.RequestID,
		record.Timestamp.Unix(),
		record.LogicalModel,
		record.SelectedTier,
		record.SelectedModel,
		record.AssistantCount,
		record.Reason,
		record.TraceDir,
		record.RequestTimeMs,
		record.StatusCode,
		record.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("insert decision record: %w", err)
	}

	// 写入成功后按 maxRecords 裁剪最旧记录，保持决策列表与分布统计
	// 始终基于最近 maxRecords 条摘要记录。
	return s.Cleanup(ctx)
}

// UpdateResult 按 request_id 回写上游转发结果（状态码与错误摘要）。
// 不存在的 request_id 视为幂等成功。
func (s *Store) UpdateResult(ctx context.Context, requestID string, statusCode int, errorMessage string) error {
	if requestID == "" {
		return fmt.Errorf("request_id is required")
	}

	_, err := s.db.ExecContext(
		ctx,
		decisionResultUpdateSQL,
		statusCode,
		errorMessage,
		requestID,
	)
	if err != nil {
		return fmt.Errorf("update decision result: %w", err)
	}
	return nil
}

func (s *Store) QueryDecisions(ctx context.Context, query DecisionQuery) (DecisionResult, error) {
	limit := query.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	where := []string{"1=1"}
	args := []any{}

	if query.LogicalModel != "" {
		where = append(where, "logical_model = ?")
		args = append(args, query.LogicalModel)
	}
	if query.Tier != "" {
		where = append(where, "selected_tier = ?")
		args = append(args, query.Tier)
	}
	if query.Query != "" {
		where = append(where, "(reason LIKE ? OR trace_dir LIKE ? OR selected_model LIKE ?)")
		like := "%" + query.Query + "%"
		args = append(args, like, like, like)
	}
	if query.Before != nil {
		where = append(where, "timestamp < ?")
		args = append(args, query.Before.Unix())
	}

	sql := fmt.Sprintf(`
SELECT
  request_id,
  timestamp,
  logical_model,
  selected_tier,
  selected_model,
  assistant_count,
  reason,
  trace_dir,
  request_time_ms,
  status_code,
  error_message
FROM decision_logs
WHERE %s
ORDER BY timestamp DESC, id DESC
LIMIT ?
`, strings.Join(where, " AND "))

	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return DecisionResult{}, fmt.Errorf("query decisions: %w", err)
	}
	defer rows.Close()

	items := make([]DecisionItem, 0)
	for rows.Next() {
		var item DecisionItem
		if err := rows.Scan(
			&item.RequestID,
			&item.Timestamp,
			&item.LogicalModel,
			&item.SelectedTier,
			&item.SelectedModel,
			&item.AssistantCount,
			&item.Reason,
			&item.TraceDir,
			&item.RequestTimeMs,
			&item.StatusCode,
			&item.ErrorMessage,
		); err != nil {
			return DecisionResult{}, fmt.Errorf("scan decision row: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return DecisionResult{}, fmt.Errorf("iterate decision rows: %w", err)
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	return DecisionResult{Items: items, HasMore: hasMore}, nil
}

func (s *Store) QueryDistribution(ctx context.Context, query DistributionQuery) (DistributionResult, error) {
	args := []any{}
	where := []string{"1=1"}

	if query.LogicalModel != "" {
		where = append(where, "logical_model = ?")
		args = append(args, query.LogicalModel)
	}
	if query.Minutes > 0 {
		cutoff := time.Now().UTC().Add(-time.Duration(query.Minutes) * time.Minute).Unix()
		where = append(where, "timestamp >= ?")
		args = append(args, cutoff)
	}

	whereClause := strings.Join(where, " AND ")

	totalSQL := fmt.Sprintf(`
SELECT COUNT(*) AS total
FROM decision_logs
WHERE %s
`, whereClause)

	var total int64
	if err := s.db.QueryRowContext(ctx, totalSQL, args...).Scan(&total); err != nil {
		return DistributionResult{}, fmt.Errorf("query distribution total: %w", err)
	}

	tierSQL := fmt.Sprintf(`
SELECT selected_tier, COUNT(*) AS tier_count
FROM decision_logs
WHERE %s
GROUP BY selected_tier
ORDER BY tier_count DESC, selected_tier ASC
`, whereClause)

	rows, err := s.db.QueryContext(ctx, tierSQL, args...)
	if err != nil {
		return DistributionResult{}, fmt.Errorf("query distribution tiers: %w", err)
	}
	defer rows.Close()

	tiers := make([]DistributionTier, 0)
	for rows.Next() {
		var tier DistributionTier
		if err := rows.Scan(&tier.Name, &tier.Count); err != nil {
			return DistributionResult{}, fmt.Errorf("scan distribution row: %w", err)
		}
		tiers = append(tiers, tier)
	}
	if err := rows.Err(); err != nil {
		return DistributionResult{}, fmt.Errorf("iterate distribution rows: %w", err)
	}

	for i := range tiers {
		if total > 0 {
			tiers[i].Ratio = float64(tiers[i].Count) / float64(total)
		}
	}

	return DistributionResult{
		Total: total,
		Tiers: tiers,
	}, nil
}

func (s *Store) Cleanup(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, decisionCountByMaxRecordsSQL, s.maxRecords)
	if err != nil {
		return fmt.Errorf("cleanup decision logs: %w", err)
	}
	return nil
}

// QueryStats 从 decision_logs 聚合出全量模型调用统计，
// 与决策记录同源、跨重启保留（区别于进程内 counter）。
func (s *Store) QueryStats(ctx context.Context) (stats.Snapshot, error) {
	var snap stats.Snapshot
	if err := s.db.QueryRowContext(ctx, decisionStatsTotalSQL).Scan(
		&snap.Total, &snap.Success, &snap.Failure,
	); err != nil {
		return snap, fmt.Errorf("query stats totals: %w", err)
	}

	models, err := s.queryModelStats(ctx, decisionStatsByModelSQL, "selected_model", "selected_model")
	if err != nil {
		return snap, fmt.Errorf("query stats by model: %w", err)
	}
	snap.Models = models

	logical, err := s.queryModelStats(ctx, decisionStatsByModelSQL, "logical_model", "logical_model")
	if err != nil {
		return snap, fmt.Errorf("query stats by logical model: %w", err)
	}
	snap.LogicalModels = logical

	return snap, nil
}

func (s *Store) queryModelStats(ctx context.Context, sqlTemplate, selectCol, groupCol string) ([]stats.ModelStats, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(sqlTemplate, selectCol, groupCol))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]stats.ModelStats, 0)
	for rows.Next() {
		var ms stats.ModelStats
		if err := rows.Scan(&ms.Model, &ms.Total, &ms.Success, &ms.Failure); err != nil {
			return nil, err
		}
		result = append(result, ms)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// ClearStats 清空全部决策记录，统计口径与决策监控随之归零。
func (s *Store) ClearStats(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, decisionClearStatsSQL); err != nil {
		return fmt.Errorf("clear stats: %w", err)
	}
	return nil
}
