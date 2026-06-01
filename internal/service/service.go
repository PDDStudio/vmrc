package service

import (
	"context"
	"fmt"
	"log"
	"time"

	ksvc "github.com/kardianos/service"
)

// stopTimeout bounds how long Stop waits for the run func to return. It is a var
// so tests can shorten it.
var stopTimeout = 12 * time.Second

// Config is the subset of service identity + behavior the Manager needs.
type Config struct {
	Name        string                          // service name (e.g. "vmrc-agent")
	DisplayName string                          // human-facing name
	Description string                          // human-facing description
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
	cancel context.CancelFunc
	done   chan error
}

func (p *program) Start(s ksvc.Service) error {
	if p.run == nil {
		return fmt.Errorf("service program has no run function")
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.done = make(chan error, 1)
	go func() { p.done <- p.run(ctx) }()
	return nil
}

func (p *program) Stop(s ksvc.Service) error {
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
