package clickhouse

import (
	"testing"
)

func TestXXH3(t *testing.T) {
	tests := []struct {
		input    string
		expected uint64
	}{
		{"a", 16629034431890738719},
		{"b", 6294355645245719615},
		{"c", 10106114510314666011},
		{"d", 5041782483466037194},
		{"e", 16566260736572803704},
		{"1", 7335560060985733464},
		{"2", 18128579709034668820},
		{"3", 8296998437054084336},
		{"", 3244421341483603138},
	}

	for _, tc := range tests {
		tc := tc // Capture range variable
		t.Run(tc.input, func(t *testing.T) {
			got := XXH3([]byte(tc.input))
			if got != tc.expected {
				t.Errorf("XXH3(%q) = %d; want %d", tc.input, got, tc.expected)
			}
		})
	}
}
