package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

type Store struct {
	db      *sql.DB
	timeout time.Duration
	maxRows int
}

func New(db *sql.DB, timeout time.Duration, maxRows int) *Store {
	return &Store{db: db, timeout: timeout, maxRows: maxRows}
}

func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	return s.db.PingContext(ctx)
}

func (s *Store) Limit(requested int) int {
	if requested <= 0 {
		if s.maxRows < 50 {
			return s.maxRows
		}
		return 50
	}
	if requested > s.maxRows {
		return s.maxRows
	}
	return requested
}

func (s *Store) Query(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", s.timeout.Milliseconds())); err != nil {
		return nil, err
	}
	// Transaction-local identity cannot leak between pooled connections. Planner
	// queries use this value for the same membership check as the Planner API.
	// Anonymous and machine tokens have no numeric suite identity and therefore
	// cannot read private plans. Administrator status never bypasses membership.
	subject := ""
	if info := auth.TokenInfoFromContext(ctx); info != nil {
		subject = info.UserID
	}
	if _, err := tx.ExecContext(ctx, `SELECT set_config('cores.user_id', $1, true)`, subject); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0)
	for rows.Next() {
		if len(result) >= s.maxRows {
			break
		}
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		item := make(map[string]any, len(columns))
		for i, column := range columns {
			item[column] = normalize(values[i])
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func normalize(value any) any {
	switch typed := value.(type) {
	case []byte:
		var decoded any
		if json.Valid(typed) && json.Unmarshal(typed, &decoded) == nil {
			return decoded
		}
		return string(typed)
	case time.Time:
		return typed.UTC().Format(time.RFC3339)
	default:
		return value
	}
}
