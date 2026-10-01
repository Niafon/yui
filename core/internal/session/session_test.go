package session

import (
	"context"
	"testing"
	"time"

	"github.com/yui-companion/core/internal/store/memstore"
)

func TestRestartContinuesTurnSequence(t *testing.T) {
	st, err := memstore.Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	for restart := 0; restart < 2; restart++ {
		m := NewManager(st.Sessions(), nil)
		for i := 1; i <= 2; i++ {
			turn, err := m.AppendTurn(ctx, "session", "user", "hello", "", "", "", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if want := restart*2 + i; turn.Seq != want {
				t.Fatalf("seq = %d, want %d", turn.Seq, want)
			}
		}
	}
}

func TestPreviousTurnCannotRemoveCurrentCancellation(t *testing.T) {
	m := NewManager(nil, nil)
	old, cancelOld := context.WithCancel(context.Background())
	defer cancelOld()
	endOld := m.BeginTurn("session", cancelOld)
	current, cancelCurrent := context.WithCancel(context.Background())
	defer cancelCurrent()
	endCurrent := m.BeginTurn("session", cancelCurrent)
	if old.Err() == nil {
		t.Fatal("previous turn was not canceled")
	}
	endOld()
	endOld() // Cleanup must be idempotent.
	if !m.Busy() {
		t.Fatal("current turn is still running")
	}
	m.CancelTurn("session")
	if current.Err() == nil {
		t.Fatal("current turn lost its cancellation")
	}
	endCurrent()
	if m.Busy() {
		t.Fatal("finished turns remain busy")
	}
}

func TestCancellationIsScopedToSession(t *testing.T) {
	m := NewManager(nil, nil)
	a, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	b, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	endA := m.BeginTurn("a", cancelA)
	endB := m.BeginTurn("b", cancelB)
	m.CancelTurn("a")
	if a.Err() == nil || b.Err() != nil {
		t.Fatal("wrong session canceled")
	}
	endA()
	if !m.Busy() {
		t.Fatal("session b is still running")
	}
	endB()
	if m.Busy() {
		t.Fatal("all turns finished")
	}
}
