package cleanup

import (
	"math"
	"testing"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{name: "zero", bytes: 0, want: "0 B"},
		{name: "one byte", bytes: 1, want: "1 B"},
		{name: "whole bytes", bytes: 42, want: "42 B"},
		{name: "last byte before KiB", bytes: 1023, want: "1023 B"},
		{name: "round down", bytes: 1075, want: "1.0 KiB"},
		{name: "round up", bytes: 1076, want: "1.1 KiB"},
		{name: "fractional KiB", bytes: 1536, want: "1.5 KiB"},
		{name: "decimal kilobyte stays bytes", bytes: 1000, want: "1000 B"},
		{name: "decimal megabyte uses KiB", bytes: 1000000, want: "976.6 KiB"},
		{name: "maximum int64", bytes: math.MaxInt64, want: "8.0 EiB"},
		{name: "minimum int64", bytes: math.MinInt64, want: "-8.0 EiB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatBytes(tt.bytes); got != tt.want {
				t.Errorf("FormatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
		// Observed free space can decrease. Formatting preserves the sign
		// and magnitude, including rounding, without overflowing MinInt64.
		if tt.bytes > 0 {
			t.Run("negative "+tt.name, func(t *testing.T) {
				if got := FormatBytes(-tt.bytes); got != "-"+tt.want {
					t.Errorf("FormatBytes(%d) = %q, want %q", -tt.bytes, got, "-"+tt.want)
				}
			})
		}
	}
}

func TestFormatBytesIECTransitions(t *testing.T) {
	for _, tt := range []struct {
		unit      string
		threshold int64
		below     string
	}{
		{unit: "KiB", threshold: 1 << 10, below: "1023 B"},
		{unit: "MiB", threshold: 1 << 20, below: "1024.0 KiB"},
		{unit: "GiB", threshold: 1 << 30, below: "1024.0 MiB"},
		{unit: "TiB", threshold: 1 << 40, below: "1024.0 GiB"},
		{unit: "PiB", threshold: 1 << 50, below: "1024.0 TiB"},
		// At this magnitude float64 rounds threshold-1 to threshold.
		{unit: "EiB", threshold: 1 << 60, below: "1.0 EiB"},
	} {
		t.Run(tt.unit, func(t *testing.T) {
			for _, boundary := range []struct {
				name  string
				bytes int64
				want  string
			}{
				{name: "below", bytes: tt.threshold - 1, want: tt.below},
				{name: "at", bytes: tt.threshold, want: "1.0 " + tt.unit},
				{name: "above", bytes: tt.threshold + 1, want: "1.0 " + tt.unit},
			} {
				t.Run(boundary.name, func(t *testing.T) {
					if got := FormatBytes(boundary.bytes); got != boundary.want {
						t.Errorf("FormatBytes(%d) = %q, want %q", boundary.bytes, got, boundary.want)
					}
					if got := FormatBytes(-boundary.bytes); got != "-"+boundary.want {
						t.Errorf("FormatBytes(%d) = %q, want %q", -boundary.bytes, got, "-"+boundary.want)
					}
				})
			}
		})
	}
}
