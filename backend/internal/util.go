package internal

import (
	"context"
	"strings"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

/* ===================== SQUIRREL HELPERS ===================== */

func toSQL(q sq.Sqlizer) (string, []any, error) {
	return q.ToSql()
}

func qRow(ctx context.Context, db *pgxpool.Pool, q sq.Sqlizer) pgx.Row {
	sql, args, err := toSQL(q)
	if err != nil {
		return db.QueryRow(ctx, "SELECT 1 WHERE 1=0")
	}
	return db.QueryRow(ctx, sql, args...)
}

func qQuery(ctx context.Context, db *pgxpool.Pool, q sq.Sqlizer) (pgx.Rows, error) {
	sql, args, err := toSQL(q)
	if err != nil {
		return nil, err
	}
	return db.Query(ctx, sql, args...)
}

func qExec(ctx context.Context, db *pgxpool.Pool, q sq.Sqlizer) (pgconn.CommandTag, error) {
	sql, args, err := toSQL(q)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	return db.Exec(ctx, sql, args...)
}

/* ===================== TX HELPERS ===================== */

func qRowTx(ctx context.Context, tx pgx.Tx, q sq.Sqlizer) pgx.Row {
	sql, args, err := toSQL(q)
	if err != nil {
		return tx.QueryRow(ctx, "SELECT 1 WHERE 1=0")
	}
	return tx.QueryRow(ctx, sql, args...)
}

func qExecTx(ctx context.Context, tx pgx.Tx, q sq.Sqlizer) (pgconn.CommandTag, error) {
	sql, args, err := toSQL(q)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	return tx.Exec(ctx, sql, args...)
}

/* ===================== LOGGING ===================== */

func logAction(db *pgxpool.Pool, actorID *int, action, details string) {
	action = strings.TrimSpace(action)
	details = strings.TrimSpace(details)

	if len([]rune(details)) > MaxLogDetails {
		r := []rune(details)
		details = string(r[:MaxLogDetails])
	}

	ins := sq.
		Insert("logs").
		Columns("actor_id", "action", "details").
		Values(actorID, action, details).
		PlaceholderFormat(sq.Dollar)

	_, _ = qExec(context.Background(), db, ins)
}
