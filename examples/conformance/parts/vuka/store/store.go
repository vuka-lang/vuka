// Package store, in plain Go, answers slowly: each call takes its time, so a page's
// parts arrive in a known order.
package store

import (
	"context"
	"errors"
	"time"
)

// Figures are an account's counts.
type Figures struct {
	Open, Shortlisted int
}

// Store is an account's data.
type Store struct{}

func wait(ctx context.Context, ms int) error {
	select {
	case <-time.After(time.Duration(ms) * time.Millisecond):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Figures are the account's counts, in 400ms.
func (Store) Figures(ctx context.Context) (Figures, error) {
	if err := wait(ctx, 400); err != nil {
		return Figures{}, err
	}
	return Figures{Open: 3, Shortlisted: 1}, nil
}

// Recent are the latest applications, in 200ms.
func (Store) Recent(ctx context.Context) ([]string, error) {
	if err := wait(ctx, 200); err != nil {
		return nil, err
	}
	return []string{"Clerk", "Driver"}, nil
}

// Tags are the account's tags, in 250ms.
func (Store) Tags(ctx context.Context) ([]string, error) {
	if err := wait(ctx, 250); err != nil {
		return nil, err
	}
	return []string{"remote", "full-time"}, nil
}

// Inbox fails after 10ms.
func (Store) Inbox(ctx context.Context) (int, error) {
	if err := wait(ctx, 10); err != nil {
		return 0, err
	}
	return 0, errors.New("inbox: dial tcp 10.0.0.7:5432: connection refused")
}

// Alerts fails after 100ms.
func (Store) Alerts(ctx context.Context) (int, error) {
	if err := wait(ctx, 100); err != nil {
		return 0, err
	}
	return 0, errors.New("alerts: timeout")
}
