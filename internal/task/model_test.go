package task

import (
	"strings"
	"testing"
)

func TestNormalizeTitle(t *testing.T) {
	if got := NormalizeTitle("  hello world  "); got != "hello world" {
		t.Errorf("NormalizeTitle() = %q, want hello world", got)
	}
}

func TestValidateFields(t *testing.T) {
	tests := []struct {
		name        string
		title       string
		description string
		wantFields  []string
	}{
		{name: "valid", title: "Task", description: "Details"},
		{name: "empty title", title: "", wantFields: []string{"title"}},
		{name: "title at limit", title: strings.Repeat("a", MaxTitleLength)},
		{name: "title over limit", title: strings.Repeat("a", MaxTitleLength+1), wantFields: []string{"title"}},
		{name: "description at limit", title: "Task", description: strings.Repeat("a", MaxDescriptionLength)},
		{name: "description over limit", title: "Task", description: strings.Repeat("a", MaxDescriptionLength+1), wantFields: []string{"description"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := ValidateFields(tt.title, tt.description)
			if len(fields) != len(tt.wantFields) {
				t.Fatalf("fields = %v, want keys %v", fields, tt.wantFields)
			}
			for _, key := range tt.wantFields {
				if _, ok := fields[key]; !ok {
					t.Errorf("fields = %v, want key %q", fields, key)
				}
			}
		})
	}
}

func TestValidateFieldsCountsRunes(t *testing.T) {
	// 200 multi-byte characters fit even though they exceed 200 bytes.
	if fields := ValidateFields(strings.Repeat("é", MaxTitleLength), ""); len(fields) != 0 {
		t.Errorf("fields = %v, want no errors for %d runes", fields, MaxTitleLength)
	}
}
