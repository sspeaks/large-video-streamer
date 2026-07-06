package detect

import (
	"errors"

	"github.com/sspeaks/large-video-streamer/internal/labels"
)

// DetectSilence returns candidate chapter boundaries from ffmpeg silencedetect. TODO(detect-silence): wrap ffmpeg silencedetect and parse output.
func DetectSilence(path string, noiseDB float64, minDur float64) ([]labels.Candidate, error) {
	return nil, errors.New("not implemented")
}
