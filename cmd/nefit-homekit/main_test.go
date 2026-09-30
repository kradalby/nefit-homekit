package main

import (
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
)

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

// A component that never finishes closing must not hold the process hostage;
// the rest still close in reverse start order before it.
func TestShutdownGivesUpOnWedgedComponent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var (
			mu     sync.Mutex
			closed []string
		)
		record := func(name string) component {
			return component{name, closerFunc(func() error {
				mu.Lock()
				defer mu.Unlock()
				closed = append(closed, name)
				return nil
			})}
		}
		release := make(chan struct{})
		wedged := component{"wedged", closerFunc(func() error {
			<-release
			return nil
		})}

		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		shutdown(logger, []component{wedged, record("a"), record("b")})

		mu.Lock()
		got := slices.Clone(closed)
		mu.Unlock()
		if want := []string{"b", "a"}; !slices.Equal(got, want) {
			t.Errorf("closed %v, want %v", got, want)
		}
		close(release)
	})
}
