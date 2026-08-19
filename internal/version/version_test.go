package version

import "testing"

func TestString(t *testing.T) {
	Version = "v1.2.3"
	BuildTime = "2026-01-01T00:00:00Z"
	if got := String("raag"); got != "raag v1.2.3 (2026-01-01T00:00:00Z)" {
		t.Errorf("String() = %q, want raag v1.2.3 (...)", got)
	}
}

func TestString_Defaults(t *testing.T) {
	Version = ""
	BuildTime = ""
	if got := String("raagd"); got == "" {
		t.Error("String() returned empty for empty metadata")
	}
}
