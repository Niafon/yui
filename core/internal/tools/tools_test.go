package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yui-companion/core/internal/model"
)

func testRegistry(t *testing.T) (*Registry, *Workspace) {
	t.Helper()
	r := NewRegistry()
	ws := NewWorkspace()
	if err := RegisterBuiltins(r, ws, nil, nil); err != nil {
		t.Fatal(err)
	}
	return r, ws
}

func TestToolMustDeclareRisk(t *testing.T) {
	r := NewRegistry()
	err := r.Register(Tool{
		Name: "x", Handler: func(context.Context, map[string]any) (string, error) { return "", nil },
	})
	if err == nil {
		t.Fatal("a tool without a risk level must not register (SRS 16.5)")
	}
}

func TestValidateRejectsUnknownAndMissingArguments(t *testing.T) {
	r, _ := testRegistry(t)
	tool, ok := r.Get("reminder.create")
	if !ok {
		t.Fatal("reminder.create should be registered")
	}
	if _, err := tool.Validate(`{"text":"позвонить","surprise":1}`); err == nil {
		t.Fatal("an invented argument must be rejected (AI-008)")
	}
	if _, err := tool.Validate(`{"in_minutes":10}`); err == nil {
		t.Fatal("a missing required argument must be rejected")
	}
	if _, err := tool.Validate(`{"text":"позвонить","in_minutes":10}`); err != nil {
		t.Fatalf("valid arguments rejected: %v", err)
	}
}

func TestDangerousToolsAreAbsent(t *testing.T) {
	r, _ := testRegistry(t)
	// SEC-013 and ADR-026: these are implemented by not existing.
	for _, name := range []string{
		"message.send", "call.dial", "payment.create", "file.delete",
		"shell.run", "emergency.call",
	} {
		if _, ok := r.Get(name); ok {
			t.Fatalf("%s must not exist in the MVP tool set", name)
		}
	}
}

func TestReminderIsCreatedAndBecomesDue(t *testing.T) {
	r, ws := testRegistry(t)
	tool, _ := r.Get("reminder.create")
	args, err := tool.Validate(`{"text":"выпить воды","in_minutes":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Handler(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if len(ws.Reminders()) != 1 {
		t.Fatal("reminder was not stored")
	}
	if len(ws.Due(time.Now())) != 0 {
		t.Fatal("a future reminder must not fire yet")
	}
	due := ws.Due(time.Now().Add(2 * time.Minute))
	if len(due) != 1 || due[0].Text != "выпить воды" {
		t.Fatalf("due = %+v, want the reminder", due)
	}
	if len(ws.Due(time.Now().Add(2*time.Minute))) != 0 {
		t.Fatal("a reminder must fire only once")
	}
}

func TestReminderIsMediumRisk(t *testing.T) {
	r, _ := testRegistry(t)
	tool, _ := r.Get("reminder.create")
	if tool.Risk != model.RiskMedium {
		t.Fatalf("risk = %v, want medium: it interrupts the owner later", tool.Risk)
	}
	timer, _ := r.Get("time.now")
	if timer.Risk != model.RiskLow {
		t.Fatal("reading the clock is low risk")
	}
}

func TestUnavailableCapabilityFailsHonestly(t *testing.T) {
	r, _ := testRegistry(t)
	tool, _ := r.Get("media.control")
	args, err := tool.Validate(`{"action":"play"}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Handler(context.Background(), args); err != ErrNotImplemented {
		t.Fatalf("error = %v, want ErrNotImplemented (NFR-010: no pretending)", err)
	}
}

func TestLinkOpenRejectsNonHTTPSchemes(t *testing.T) {
	r, _ := testRegistry(t)
	tool, _ := r.Get("link.open")
	args, _ := tool.Validate(`{"url":"file:///C:/Windows/System32"}`)
	if _, err := tool.Handler(context.Background(), args); err == nil {
		t.Fatal("only http and https may be opened")
	}
}

func TestDescribeIsHumanReadable(t *testing.T) {
	r, _ := testRegistry(t)
	tool, _ := r.Get("reminder.create")
	text := Describe(tool, map[string]any{"text": "позвонить маме", "in_minutes": float64(30)})
	if !strings.Contains(text, "позвонить маме") {
		t.Fatalf("confirmation text must describe the action, got %q", text)
	}
}

func TestPendingInvocationsExpire(t *testing.T) {
	p := NewPending()
	p.ttl = 10 * time.Millisecond
	inv := &Invocation{ID: "inv_1", Tool: "reminder.create"}
	p.Add(inv)
	if _, ok := p.Take("inv_1"); !ok {
		t.Fatal("invocation should be retrievable immediately")
	}
	p.Add(&Invocation{ID: "inv_2"})
	time.Sleep(20 * time.Millisecond)
	if _, ok := p.Take("inv_2"); ok {
		t.Fatal("an unanswered confirmation must expire, not stay executable")
	}
}

func TestResolveTimeRequiresAnUnambiguousInput(t *testing.T) {
	if _, err := resolveTime(map[string]any{}); err == nil {
		t.Fatal("no time at all must be an error, not a guess")
	}
	at, err := resolveTime(map[string]any{"in_minutes": float64(15)})
	if err != nil || time.Until(at) > 16*time.Minute {
		t.Fatalf("in_minutes not applied: %v %v", at, err)
	}
}
