package labels

import (
	"fmt"
	"strings"
)

func toWebVTT(labels VideoLabels) string {
	boundaries := sortedBoundaries(labels.Boundaries)
	if len(boundaries) == 0 {
		return "WEBVTT\n\n"
	}

	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for i, boundary := range boundaries {
		end := boundary.Start + 600
		if i+1 < len(boundaries) {
			end = boundaries[i+1].Start
		}
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", i+1, secondsToVTT(boundary.Start), secondsToVTT(end), boundary.Name)
	}
	return b.String()
}

func secondsToVTT(seconds float64) string {
	milliseconds := int(seconds*1000 + 0.5)
	if milliseconds < 0 {
		milliseconds = 0
	}
	hours := milliseconds / 3_600_000
	milliseconds %= 3_600_000
	minutes := milliseconds / 60_000
	milliseconds %= 60_000
	secs := milliseconds / 1_000
	milliseconds %= 1_000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, secs, milliseconds)
}
