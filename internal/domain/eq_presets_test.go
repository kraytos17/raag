package domain

import "testing"

func TestEQPresets_AllValid(t *testing.T) {
	if len(EQPresets) == 0 {
		t.Fatal("expected at least one EQ preset")
	}
	for i, p := range EQPresets {
		if !p.Enabled {
			t.Errorf("preset %d: Enabled = false, want true", i)
		}
		for name, v := range map[string]float64{"bass": p.Bass, "mid": p.Mid, "treble": p.Treble} {
			if v < -12 || v > 12 {
				t.Errorf("preset %d: %s = %v, want within [-12, 12]", i, name, v)
			}
		}
	}
}

func TestEQPresets_AllDistinct(t *testing.T) {
	for i := range EQPresets {
		for j := i + 1; j < len(EQPresets); j++ {
			if EQPresets[i] == EQPresets[j] {
				t.Errorf("presets %d and %d are identical", i, j)
			}
		}
	}
}
