package terminal

import "testing"

func TestTrimScreenTail(t *testing.T) {
	tests := []struct {
		name  string
		input string
		n     int
		want  string
	}{
		{"fewer lines than n", "a\nb\nc", 5, "a\nb\nc"},
		{"exact lines", "a\nb\nc", 3, "a\nb\nc"},
		{"more lines than n", "a\nb\nc\nd\ne", 3, "c\nd\ne"},
		{"blank tail anchored to content", "top\nbody\n\n\n\n", 3, "top\nbody"},
		{"single line", "hello", 3, "hello"},
		{"empty string", "", 3, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TrimScreenTail(tt.input, tt.n)
			if got != tt.want {
				t.Errorf("TrimScreenTail(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
			}
		})
	}
}
