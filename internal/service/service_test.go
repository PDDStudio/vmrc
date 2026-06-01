package service

import (
	"context"
	"testing"
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

func TestNewBuildsManager(t *testing.T) {
	m, err := New(Config{Name: "vmrc-agent", Run: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatalf("New err = %v", err)
	}
	if m == nil {
		t.Fatal("New returned nil manager")
	}
}
