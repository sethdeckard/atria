package agent

import "testing"

func TestInstalledReturnsKnownTypes(t *testing.T) {
	// What is installed depends on the machine; the result must still be a
	// subset of Types() with no duplicates.
	known := make(map[Type]bool)
	for _, typ := range Types() {
		known[typ] = true
	}
	seen := make(map[Type]bool)
	for _, typ := range Installed() {
		if !known[typ] {
			t.Errorf("unexpected agent type: %q", typ)
		}
		if seen[typ] {
			t.Errorf("duplicate agent type: %q", typ)
		}
		seen[typ] = true
	}
}

func TestTypesReturnsCopy(t *testing.T) {
	a := Types()
	a[0] = "mutated"
	if b := Types(); b[0] == "mutated" {
		t.Fatal("Types() returned shared backing storage")
	}
	if len(a) != 4 {
		t.Fatalf("expected 4 types, got %d", len(a))
	}
}
