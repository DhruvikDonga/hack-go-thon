package pgstore

import (
	"math"
	"testing"
)

func TestFormatAndParseVector(t *testing.T) {
	tests := []struct {
		name string
		vec  []float32
	}{
		{
			name: "standard vector",
			vec:  []float32{0.1, -0.25, 0.333, 0.9999},
		},
		{
			name: "single component",
			vec:  []float32{1.0},
		},
		{
			name: "empty vector",
			vec:  []float32{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatted := FormatVector(tt.vec)
			parsed, err := ParseVector(formatted)
			if err != nil {
				t.Fatalf("unexpected error parsing vector: %v", err)
			}

			if len(tt.vec) == 0 {
				if len(parsed) != 0 {
					t.Fatalf("expected empty vector, got %v", parsed)
				}
				return
			}

			if len(parsed) != len(tt.vec) {
				t.Fatalf("length mismatch: expected %d, got %d", len(tt.vec), len(parsed))
			}

			for i := range tt.vec {
				diff := math.Abs(float64(parsed[i] - tt.vec[i]))
				if diff > 1e-5 {
					t.Errorf("at index %d: expected %f, got %f", i, tt.vec[i], parsed[i])
				}
			}
		})
	}
}

func TestParseVectorErrors(t *testing.T) {
	invalid := "[0.1, abc, 0.3]"
	_, err := ParseVector(invalid)
	if err == nil {
		t.Errorf("expected error parsing invalid vector string, got nil")
	}
}
