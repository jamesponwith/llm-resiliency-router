package main

import "testing"

func TestGreet(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"default", "", "hello, world"},
		{"named", "flywheel", "hello, flywheel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Greet(tt.in); got != tt.want {
				t.Errorf("Greet(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
