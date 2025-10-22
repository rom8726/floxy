package floxy

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Tx interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

type TxManager interface {
	ReadCommitted(ctx context.Context, fn func(ctx context.Context) error) error
	RepeatableRead(ctx context.Context, fn func(ctx context.Context) error) error
}

type TxManagerImpl struct {
	pool *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) *TxManagerImpl {
	return &TxManagerImpl{
		pool: pool,
	}
}

func (m *TxManagerImpl) ReadCommitted(ctx context.Context, fn func(ctx context.Context) error) error {
	return m.run(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, fn)
}

func (m *TxManagerImpl) RepeatableRead(ctx context.Context, fn func(ctx context.Context) error) error {
	return m.run(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead}, fn)
}

func (m *TxManagerImpl) run(ctx context.Context, opts pgx.TxOptions, fn func(ctx context.Context) error) error {
	if txOpened := TxFromContext(ctx); txOpened != nil {
		return fn(ctx)
	}

	tx, err := m.pool.BeginTx(ctx, opts)
	if err != nil {
		return err
	}

	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback(ctx)

			panic(r)
		}
	}()

	txCtx := context.WithValue(ctx, txKey{}, tx)

	if err := fn(txCtx); err != nil {
		_ = tx.Rollback(ctx)

		return err
	}

	return tx.Commit(ctx)
}
