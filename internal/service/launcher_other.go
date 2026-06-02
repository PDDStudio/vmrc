//go:build !windows

package service

import ksvc "github.com/kardianos/service"

type kardianosLauncher struct{ svc ksvc.Service }

func newOSLauncher(spec LaunchSpec) (Launcher, error) {
	cfg := &ksvc.Config{
		Name:        spec.Name,
		DisplayName: "vmrc agent (user)",
		Description: "vmrc VM remote-control agent (user session)",
		Arguments:   spec.Args,
		Executable:  spec.Executable,
		Option:      ksvc.KeyValue{"UserService": true},
	}
	svc, err := ksvc.New(&program{}, cfg)
	if err != nil {
		return nil, err
	}
	return kardianosLauncher{svc: svc}, nil
}

func (l kardianosLauncher) Install() error   { return ksvc.Control(l.svc, "install") }
func (l kardianosLauncher) Uninstall() error { return ksvc.Control(l.svc, "uninstall") }
func (l kardianosLauncher) Start() error     { return ksvc.Control(l.svc, "start") }
func (l kardianosLauncher) Stop() error      { return ksvc.Control(l.svc, "stop") }

func (l kardianosLauncher) Status() (string, error) {
	st, err := l.svc.Status()
	if err != nil {
		return "unknown", nil
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
