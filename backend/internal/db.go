package internal

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func MustDB(url string) *pgxpool.Pool {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		log.Fatal(err)
	}
	cfg.MaxConns = 10

	var pool *pgxpool.Pool

	// ждём БД до ~30 секунд
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
