package layout

// Width tiers are shared across TUI surfaces so behavior is predictable on
// laptops, wide displays, and ultra‑wide monitors.
//
// The values mirror patterns taken from beads_viewer:
//   - SplitView: when to switch from stacked to split layouts
//   - WideView: when to start surfacing secondary metadata columns
//   - UltraWideView: when to show tertiary metadata (labels, models, etc.)
const (
	SplitViewThreshold     = 110
	WideViewThreshold      = 140
	UltraWideViewThreshold = 180
)

// Tier describes the current width bucket.
type Tier int

const (
	TierNarrow Tier = iota
	TierSplit
	TierWide
	TierUltra
)

// TierForWidth maps a terminal width to a tier.
func TierForWidth(width int) Tier {
	switch {
	case width >= UltraWideViewThreshold:
		return TierUltra
	case width >= WideViewThreshold:
		return TierWide
	case width >= SplitViewThreshold:
		return TierSplit
	default:
		return TierNarrow
	}
}

// TruncateRunes trims a string to max runes and appends suffix if truncated.
// It is rune‑aware to avoid splitting emoji or wide glyphs.
func TruncateRunes(s string, max int, suffix string) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max < len([]rune(suffix)) {
		return string(runes[:max])
	}
	return string(runes[:max-len([]rune(suffix))]) + suffix
}

// SplitProportions returns left/right widths for split view given total width.
// It removes a small padding budget to prevent edge wrapping.
func SplitProportions(total int) (left int, right int) {
	if total < SplitViewThreshold {
		return total, 0
	}
	// Budget 4 cols for borders/padding on each panel (8 total)
	avail := total - 8
	if avail < 10 {
		avail = total
	}
	left = int(float64(avail) * 0.4)
	right = avail - left
	return
}
