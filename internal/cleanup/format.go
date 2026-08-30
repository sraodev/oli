package cleanup

import "fmt"

// FormatBytes renders bytes using binary IEC units.
func FormatBytes(bytes int64) string {
	sign := ""
	value := float64(bytes)
	if value < 0 {
		sign = "-"
		value = -value
	}
	if value < 1024 {
		if bytes < 0 {
			return fmt.Sprintf("-%d B", -bytes)
		}
		return fmt.Sprintf("%d B", bytes)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	for _, unit := range units {
		value /= 1024
		if value < 1024 || unit == units[len(units)-1] {
			return fmt.Sprintf("%s%.1f %s", sign, value, unit)
		}
	}
	return fmt.Sprintf("%d B", bytes)
}
