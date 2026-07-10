package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	ksvc "github.com/kardianos/service"
)

// stopTimeout bounds how long Stop waits for the run func to return. It is a var
// so tests can shorten it.
var stopTimeout = 12 * time.Second

// osExit is a seam so tests can observe the process-exit path without dying.
var osExit = os.Exit

// defaultOnExit is invoked when the run func returns on its own — i.e. the agent
// stopped serving while the supervisor still believes it is running. It logs and
// exits the process non-zero so the OS supervisor's restart policy (and the
// user-mode scheduled-task keepalive) actually fire; a run func that merely
// parks its error would leave a dead agent reported as "running".
func defaultOnExit(err error) {
	if err != nil {
		log.Printf("service: run function exited unexpectedly: %v", err)
	} else {
		log.Printf("service: run function exited unexpectedly")
	}
	osExit(1)
}

// Config is the subset of service identity + behavior the Manager needs.
type Config struct {
	Name        string                          // service name (e.g. "vmrc-agent")
	DisplayName string                          // human-facing name
	Description string                          // human-facing description
	Executable  string                          // absolute path the OS launches (empty → os.Executable())
	Arguments   []string                        // args the OS uses to launch the service body
	Run         func(ctx context.Context) error // the agent serving func (nil for control-only use)
}

// toKardianosConfig maps our Config onto the kardianos service.Config. Run is
// not part of the kardianos config (it lives in the program); only identity and
// launch arguments are mapped.
func toKardianosConfig(cfg Config) *ksvc.Config {
	return &ksvc.Config{
		Name:        cfg.Name,
		DisplayName: cfg.DisplayName,
		Description: cfg.Description,
		Executable:  cfg.Executable,
		Arguments:   cfg.Arguments,
		// OnFailure is honored only on Windows; on Linux (systemd) and macOS
		// (launchd) kardianos auto-restarts via its defaults (Restart=always /
		// KeepAlive), so cross-platform restart still holds.
		Option: ksvc.KeyValue{"OnFailure": "restart"},
	}
}

// program implements kardianos service.Interface. Start launches the run func on
// a cancelable context and returns immediately (kardianos requires Start to be
// non-blocking); Stop cancels and waits for it to return.
type program struct {
	run    func(ctx context.Context) error
	onExit func(err error) // called when run returns before Stop; nil → defaultOnExit
	cancel context.CancelFunc
	done   chan error

	mu       sync.Mutex
	stopping bool
}

func (p *program) Start(s ksvc.Service) error {
	if p.run == nil {
		return fmt.Errorf("service program has no run function")
	}
	if p.onExit == nil {
		p.onExit = defaultOnExit
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.done = make(chan error, 1)
	go func() {
		err := p.run(ctx)
		p.mu.Lock()
		stopping := p.stopping
		p.mu.Unlock()
		if stopping {
			// Expected: Stop cancelled us and is waiting on p.done.
			p.done <- err
			return
		}
		// Unexpected: the agent stopped serving while the supervisor still
		// thinks it is running. Surface it so the restart policy can act.
		p.onExit(err)
	}()
	return nil
}

func (p *program) Stop(s ksvc.Service) error {
	p.mu.Lock()
	p.stopping = true
	p.mu.Unlock()
	if p.cancel != nil {
		p.cancel()
	}
	if p.done == nil {
		return nil
	}
	select {
	case err := <-p.done:
		return err
	case <-time.After(stopTimeout):
		log.Printf("service: run func did not stop within %s", stopTimeout)
		return nil
	}
}

// Manager wraps a kardianos service plus its program.
type Manager struct {
	svc ksvc.Service
}

// New builds a Manager from cfg.
func New(cfg Config) (*Manager, error) {
	prg := &program{run: cfg.Run}
	svc, err := ksvc.New(prg, toKardianosConfig(cfg))
	if err != nil {
		return nil, fmt.Errorf("building service: %w", err)
	}
	return &Manager{svc: svc}, nil
}

// Run runs the service body. kardianos auto-detects interactive vs. supervised
// execution; interactively it installs its own SIGINT/SIGTERM handling and calls
// Stop on signal.
func (m *Manager) Run() error { return m.svc.Run() }

// Control performs a lifecycle action: "install", "uninstall", "start", "stop".
func (m *Manager) Control(action string) error {
	if err := ksvc.Control(m.svc, action); err != nil {
		return fmt.Errorf("service %s: %w", action, err)
	}
	return nil
}

// Status returns "running", "stopped", or "unknown".
func (m *Manager) Status() (string, error) {
	st, err := m.svc.Status()
	if err != nil {
		return "unknown", fmt.Errorf("service status: %w", err)
	}
	switch st {
	case ksvc.StatusRunning:
		return "running", nil
	case ksvc.StatusStopped:
		return "stopped", nil
	default:
		return "unknown", nil
	}
}
