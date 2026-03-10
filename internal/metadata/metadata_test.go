package metadata

import (
	"testing"
)

func TestFormatMetadata(t *testing.T) {
	song := Song{
		Title:  "Test Song",
		Artist: "Test Artist",
		Album:  "Test Album",
	}

	result := FormatMetadata(song)
	expected := "Test Song|Test Artist|Test Album"

	if result != expected {
		t.Errorf("FormatMetadata() = %v, want %v", result, expected)
	}
}

func TestFormatMetadataWithEmptyFields(t *testing.T) {
	song := Song{
		Title:  "Test",
		Artist: "",
		Album:  "",
	}

	result := FormatMetadata(song)
	expected := "Test||"

	if result != expected {
		t.Errorf("FormatMetadata() = %v, want %v", result, expected)
	}
}
