package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestToKardianosConfigMapping(t *testing.T) {
	cfg := Config{
		Name:        "vmrc-agent",
		DisplayName: "vmrc agent",
		Description: "vmrc VM remote-control agent",
		Arguments:   []string{"run", "--config", "/etc/vmrc/agent.json"},
	}
	k := toKardianosConfig(cfg)
	if k.Name != "vmrc-agent" {
		t.Errorf("Name = %q", k.Name)
	}
	if k.DisplayName != "vmrc agent" {
		t.Errorf("DisplayName = %q", k.DisplayName)
	}
	if len(k.Arguments) != 3 || k.Arguments[0] != "run" || k.Arguments[2] != "/etc/vmrc/agent.json" {
		t.Errorf("Arguments = %v", k.Arguments)
	}
}

func TestToKardianosConfigMapsExecutable(t *testing.T) {
	const managed = "/opt/vmrc/bin/vmrc-agent"
	k := toKardianosConfig(Config{Name: "vmrc-agent", Executable: managed})
	if k.Executable != managed {
		t.Errorf("Executable = %q, want %q", k.Executable, managed)
	}
}

func TestNewBuildsManager(t *testing.T) {
	m, err := New(Config{Name: "vmrc-agent", Run: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatalf("New err = %v", err)
	}
	if m == nil {
		t.Fatal("New returned nil manager")
	}
}

// These program lifecycle tests mutate the package-level stopTimeout var, so
// they must not be made parallel (no t.Parallel()).

func TestProgramStartRequiresRunFunc(t *testing.T) {
	p := &program{}
	if err := p.Start(nil); err == nil {
		t.Fatal("Start with nil run func should error")
	}
}

func TestProgramStopBeforeStartIsNoop(t *testing.T) {
	p := &program{}
	if err := p.Stop(nil); err != nil {
		t.Fatalf("Stop before Start should be nil, got %v", err)
	}
}

func TestProgramStopReturnsRunError(t *testing.T) {
	wantErr := errors.New("boom")
	p := &program{run: func(ctx context.Context) error {
		<-ctx.Done()
		return wantErr
	}}
	if err := p.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := p.Stop(nil); !errors.Is(err, wantErr) {
		t.Fatalf("Stop returned %v, want %v", err, wantErr)
	}
}

func TestProgramUnexpectedExitTriggersOnExit(t *testing.T) {
	wantErr := errors.New("serve died")
	got := make(chan error, 1)
	p := &program{
		run:    func(ctx context.Context) error { return wantErr },
		onExit: func(err error) { got <- err },
	}
	if err := p.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case err := <-got:
		if !errors.Is(err, wantErr) {
			t.Fatalf("onExit got %v, want %v", err, wantErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onExit was not called after run returned on its own")
	}
}

func TestProgramStopDoesNotTriggerOnExit(t *testing.T) {
	called := make(chan struct{}, 1)
	p := &program{
		run:    func(ctx context.Context) error { <-ctx.Done(); return nil },
		onExit: func(err error) { called <- struct{}{} },
	}
	if err := p.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := p.Stop(nil); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-called:
		t.Fatal("onExit fired during a normal Stop-driven shutdown")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestDefaultOnExitExitsNonZero(t *testing.T) {
	old := osExit
	got := make(chan int, 1)
	osExit = func(code int) { got <- code }
	defer func() { osExit = old }()

	defaultOnExit(errors.New("boom"))
	if code := <-got; code != 1 {
		t.Fatalf("defaultOnExit exit code = %d, want 1", code)
	}
}

func TestProgramStopHonorsTimeout(t *testing.T) {
	old := stopTimeout
	stopTimeout = 50 * time.Millisecond
	defer func() { stopTimeout = old }()

	// run func ignores ctx and outlives the timeout; Stop must still return.
	p := &program{run: func(ctx context.Context) error {
		time.Sleep(500 * time.Millisecond)
		return nil
	}}
	if err := p.Start(nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- p.Stop(nil) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop on timeout should return nil, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return after timeout")
	}
}
