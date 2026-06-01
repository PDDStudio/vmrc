package main

import "testing"

func TestServiceArgs(t *testing.T) {
	args := serviceArgs("/etc/vmrc/agent.json", "vmrc-agent")
	want := []string{"run", "--config", "/etc/vmrc/agent.json", "--service-name", "vmrc-agent"}
	if len(args) != len(want) {
		t.Fatalf("serviceArgs len = %d, want %d (%v)", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want[i])
		}
	}
}

func TestServiceArgsCustomName(t *testing.T) {
	args := serviceArgs("/c/agent.json", "foo")
	// the baked --service-name must match the registered name so the launched
	// body's kardianos identity matches the installed service.
	found := false
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--service-name" && args[i+1] == "foo" {
			found = true
		}
	}
	if !found {
		t.Errorf("serviceArgs did not bake --service-name foo: %v", args)
	}
}
