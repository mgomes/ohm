package sqltx

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
)

var driverID atomic.Int64

func TestDoCommitsWhenFunctionSucceeds(t *testing.T) {
	db, recorder := openTestDB(t, txBehavior{})

	err := Do(context.Background(), db, nil, func(tx *sql.Tx) error {
		if tx == nil {
			t.Errorf("Do(ctx, db, nil, fn) tx = nil, want non-nil")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do(ctx, db, nil, fn) error = %v, want nil", err)
	}

	want := []string{"begin", "commit"}
	if got := recorder.events(); !slices.Equal(got, want) {
		t.Errorf("Do(ctx, db, nil, fn) events = %v, want %v", got, want)
	}
}

func TestDoRollsBackWhenFunctionFails(t *testing.T) {
	runErr := errors.New("run failed")
	db, recorder := openTestDB(t, txBehavior{})

	err := Do(context.Background(), db, nil, func(*sql.Tx) error {
		return runErr
	})
	if !errors.Is(err, runErr) {
		t.Fatalf("Do(ctx, db, nil, fn) error = %v, want %v", err, runErr)
	}

	want := []string{"begin", "rollback"}
	if got := recorder.events(); !slices.Equal(got, want) {
		t.Errorf("Do(ctx, db, nil, fn) events = %v, want %v", got, want)
	}
}

func TestDoJoinsRollbackErrorWithFunctionError(t *testing.T) {
	runErr := errors.New("run failed")
	rollbackErr := errors.New("rollback failed")
	db, _ := openTestDB(t, txBehavior{rollbackErr: rollbackErr})

	err := Do(context.Background(), db, nil, func(*sql.Tx) error {
		return runErr
	})
	if !errors.Is(err, runErr) {
		t.Errorf("Do(ctx, db, nil, fn) error = %v, want run error", err)
	}
	if !errors.Is(err, rollbackErr) {
		t.Errorf("Do(ctx, db, nil, fn) error = %v, want rollback error", err)
	}
}

func TestDoReturnsCommitError(t *testing.T) {
	commitErr := errors.New("commit failed")
	db, recorder := openTestDB(t, txBehavior{commitErr: commitErr})

	err := Do(context.Background(), db, nil, func(*sql.Tx) error {
		return nil
	})
	if !errors.Is(err, commitErr) {
		t.Fatalf("Do(ctx, db, nil, fn) error = %v, want %v", err, commitErr)
	}

	want := []string{"begin", "commit"}
	if got := recorder.events(); !slices.Equal(got, want) {
		t.Errorf("Do(ctx, db, nil, fn) events = %v, want %v", got, want)
	}
}

func TestDoReturnsBeginError(t *testing.T) {
	beginErr := errors.New("begin failed")
	db, recorder := openTestDB(t, txBehavior{beginErr: beginErr})

	err := Do(context.Background(), db, nil, func(*sql.Tx) error {
		t.Fatalf("Do(ctx, db, nil, fn) called fn after begin error")
		return nil
	})
	if !errors.Is(err, beginErr) {
		t.Fatalf("Do(ctx, db, nil, fn) error = %v, want %v", err, beginErr)
	}

	want := []string{"begin"}
	if got := recorder.events(); !slices.Equal(got, want) {
		t.Errorf("Do(ctx, db, nil, fn) events = %v, want %v", got, want)
	}
}

func TestDoRollsBackBeforeRepanic(t *testing.T) {
	db, recorder := openTestDB(t, txBehavior{})
	panicValue := "boom"

	defer func() {
		got := recover()
		if got != panicValue {
			t.Fatalf("Do(ctx, db, nil, fn) panic = %v, want %v", got, panicValue)
		}

		want := []string{"begin", "rollback"}
		if events := recorder.events(); !slices.Equal(events, want) {
			t.Errorf("Do(ctx, db, nil, fn) events = %v, want %v", events, want)
		}
	}()

	_ = Do(context.Background(), db, nil, func(*sql.Tx) error {
		panic(panicValue)
	})
}

func TestDoValidatesInputs(t *testing.T) {
	err := Do(context.Background(), nil, nil, func(*sql.Tx) error {
		return nil
	})
	if err == nil {
		t.Fatalf("Do(ctx, nil, nil, fn) error = nil, want non-nil")
	}

	db, _ := openTestDB(t, txBehavior{})
	err = Do(context.Background(), db, nil, nil)
	if err == nil {
		t.Fatalf("Do(ctx, db, nil, nil) error = nil, want non-nil")
	}
}

type txBehavior struct {
	beginErr    error
	commitErr   error
	rollbackErr error
}

type txRecorder struct {
	eventsList []string
}

func (r *txRecorder) append(event string) {
	r.eventsList = append(r.eventsList, event)
}

func (r *txRecorder) events() []string {
	return slices.Clone(r.eventsList)
}

func openTestDB(t *testing.T, behavior txBehavior) (*sql.DB, *txRecorder) {
	t.Helper()

	recorder := &txRecorder{}
	driverName := fmt.Sprintf("ohm_sqltx_test_%d", driverID.Add(1))
	sql.Register(driverName, &txDriver{
		behavior: behavior,
		recorder: recorder,
	})

	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("sql.Open(%q) error = %v, want nil", driverName, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close() error = %v, want nil", err)
		}
	})
	return db, recorder
}

type txDriver struct {
	behavior txBehavior
	recorder *txRecorder
}

func (d *txDriver) Open(string) (driver.Conn, error) {
	return &txConn{
		behavior: d.behavior,
		recorder: d.recorder,
	}, nil
}

type txConn struct {
	behavior txBehavior
	recorder *txRecorder
}

func (c *txConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("prepare is not implemented")
}

func (c *txConn) Close() error {
	return nil
}

func (c *txConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *txConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	c.recorder.append("begin")
	if c.behavior.beginErr != nil {
		return nil, c.behavior.beginErr
	}
	return &testTx{
		behavior: c.behavior,
		recorder: c.recorder,
	}, nil
}

type testTx struct {
	behavior txBehavior
	recorder *txRecorder
}

func (tx *testTx) Commit() error {
	tx.recorder.append("commit")
	return tx.behavior.commitErr
}

func (tx *testTx) Rollback() error {
	tx.recorder.append("rollback")
	return tx.behavior.rollbackErr
}
