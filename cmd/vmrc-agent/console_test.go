package main

import "testing"

func TestOwnsConsole(t *testing.T) {
	cases := map[int]bool{0: false, 1: true, 2: false, 5: false}
	for n, want := range cases {
		if got := ownsConsole(n); got != want {
			t.Errorf("ownsConsole(%d) = %v, want %v", n, got, want)
		}
	}
}
