// Package sqltx provides explicit transaction helpers for database/sql.
package sqltx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Do runs fn inside a database transaction.
//
// The transaction commits only when fn returns nil. If fn returns an error, Do
// rolls the transaction back and returns that error. The caller owns db and
// remains responsible for closing it.
func Do(ctx context.Context, db *sql.DB, opts *sql.TxOptions, fn func(*sql.Tx) error) (err error) {
	if db == nil {
		return fmt.Errorf("database is required")
	}
	if fn == nil {
		return fmt.Errorf("transaction function is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	finished := false
	defer func() {
		if p := recover(); p != nil {
			if !finished {
				_ = tx.Rollback()
			}
			panic(p)
		}
		if finished {
			return
		}

		rollbackErr := tx.Rollback()
		if rollbackErr == nil {
			return
		}
		wrapped := fmt.Errorf("rollback transaction: %w", rollbackErr)
		if err != nil {
			err = errors.Join(err, wrapped)
			return
		}
		err = wrapped
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		finished = true
		return fmt.Errorf("commit transaction: %w", err)
	}
	finished = true
	return nil
}
