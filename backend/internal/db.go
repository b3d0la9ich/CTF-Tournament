package internal

import (
	"context"
	"log"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

/* ===================== CONNECT ===================== */

func MustDB(url string) *pgxpool.Pool {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		log.Fatal(err)
	}
	cfg.MaxConns = 10

	var pool *pgxpool.Pool

	deadline := time.Now().Add(30 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		pool, err = pgxpool.NewWithConfig(ctx, cfg)
		if err == nil {
			if pingErr := pool.Ping(ctx); pingErr == nil {
				cancel()
				break
			}
			pool.Close()
			err = ctx.Err()
		}
		cancel()

		if time.Now().After(deadline) {
			log.Fatalf("failed to connect DB after retries: %v", err)
		}
		time.Sleep(1 * time.Second)
	}

	return pool
}

/* ===================== SQUIRREL HELPERS ===================== */

// ----------- NON-TX -----------

func qExec(ctx context.Context, db *pgxpool.Pool, q sq.Sqlizer) (pgconn.CommandTag, error) {
	sql, args, err := q.ToSql()
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	return db.Exec(ctx, sql, args...)
}

func qQuery(ctx context.Context, db *pgxpool.Pool, q sq.SelectBuilder) (pgx.Rows, error) {
	sql, args, err := q.ToSql()
	if err != nil {
		return nil, err
	}
	return db.Query(ctx, sql, args...)
}

func qRow(ctx context.Context, db *pgxpool.Pool, q sq.SelectBuilder) pgx.Row {
	sql, args, _ := q.ToSql()
	return db.QueryRow(ctx, sql, args...)
}

// ----------- TX -----------

func qExecTx(ctx context.Context, tx pgx.Tx, q sq.Sqlizer) (pgconn.CommandTag, error) {
	sql, args, err := q.ToSql()
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	return tx.Exec(ctx, sql, args...)
}

func qQueryTx(ctx context.Context, tx pgx.Tx, q sq.SelectBuilder) (pgx.Rows, error) {
	sql, args, err := q.ToSql()
	if err != nil {
		return nil, err
	}
	return tx.Query(ctx, sql, args...)
}

func qRowTx(ctx context.Context, tx pgx.Tx, q sq.SelectBuilder) pgx.Row {
	sql, args, _ := q.ToSql()
	return tx.QueryRow(ctx, sql, args...)
}
