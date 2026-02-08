package internal

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func logAction(db *pgxpool.Pool, actorID *int, action, details string) {
	_, _ = db.Exec(context.Background(),
		"INSERT INTO logs(actor_id, action, details) VALUES ($1,$2,$3)",
		actorID, action, details,
	)
}
