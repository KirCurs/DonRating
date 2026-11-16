package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, connString)

	if err != nil {
		return nil, err
	}
	commCheck := pool.Ping(ctx)
	if commCheck != nil {
		pool.Close()
		return nil, commCheck
	}
	return pool, nil
}
