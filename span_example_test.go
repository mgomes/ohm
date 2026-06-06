package ohm

import (
	"context"
	"time"
)

func ExampleObserve() {
	ctx := context.Background()

	user, err := Observe(ctx, "load user", func(ctx context.Context) (string, error) {
		return "ada", nil
	}, SlowAfter(50*time.Millisecond))
	if err != nil {
		return
	}

	_ = user
}
