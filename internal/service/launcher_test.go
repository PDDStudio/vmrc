package service

import "testing"

type fakeLauncher struct {
	actions []string
	status  string
}

func (f *fakeLauncher) Install() error          { f.actions = append(f.actions, "install"); return nil }
func (f *fakeLauncher) Uninstall() error        { f.actions = append(f.actions, "uninstall"); return nil }
func (f *fakeLauncher) Start() error            { f.actions = append(f.actions, "start"); return nil }
func (f *fakeLauncher) Stop() error             { f.actions = append(f.actions, "stop"); return nil }
func (f *fakeLauncher) Status() (string, error) { return f.status, nil }

func TestLauncherInterfaceSatisfied(t *testing.T) {
	var _ Launcher = (*fakeLauncher)(nil)
}
