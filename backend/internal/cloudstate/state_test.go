package cloudstate

import (
	"context"
	"errors"
	"github.com/solarhell/certship/internal/discovery"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefreshCoalescesAndInvalidates(t *testing.T) {
	c := newCache()
	var calls atomic.Int32
	load := func() (discovery.Result, error) { calls.Add(1); return discovery.Result{ObservedAt: time.Now()}, nil }
	var group sync.WaitGroup
	for range 20 {
		group.Go(func() {
			if _, err := c.refresh(context.Background(), [32]byte{1}, false, load); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if calls.Load() != 1 {
		t.Fatalf("concurrent refreshes called cloud %d times", calls.Load())
	}
	if _, err := c.refresh(context.Background(), [32]byte{2}, false, load); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("account/config change reused previous inventory")
	}
	if _, err := c.refresh(context.Background(), [32]byte{2}, true, load); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatal("explicit user refresh did not query cloud")
	}
}
func TestRefreshFailureDoesNotReturnOldInventory(t *testing.T) {
	c := newCache()
	key := [32]byte{1}
	old := discovery.Result{ObservedAt: time.Now()}
	_, _ = c.refresh(context.Background(), key, true, func() (discovery.Result, error) { return old, nil })
	failure := errors.New("cloud unavailable")
	result, err := c.refresh(context.Background(), key, true, func() (discovery.Result, error) { return discovery.Result{}, failure })
	if !errors.Is(err, failure) || !result.ObservedAt.IsZero() {
		t.Fatal("failed explicit refresh returned stale cloud state")
	}
}
func TestRefreshWaitIsCancellable(t *testing.T) {
	c := newCache()
	c.gate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.refresh(ctx, [32]byte{}, false, func() (discovery.Result, error) {
		t.Fatal("cancelled waiter scanned cloud")
		return discovery.Result{}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
