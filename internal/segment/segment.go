package segment

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sspeaks/large-video-streamer/internal/config"
)

// Segment generates HLS output for videoName.
func Segment(cfg config.Config, videoName string) error {
	src := filepath.Join(cfg.VideoDir, videoName+".mkv")
	if _, err := os.Stat(src); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("source video %q does not exist", src)
		}
		return fmt.Errorf("stat source video %q: %w", src, err)
	}

	outDir := filepath.Join(cfg.HLSDir, videoName)
	playlist := filepath.Join(outDir, "playlist.m3u8")
	complete, err := playlistExists(playlist)
	if err != nil {
		return err
	}
	if complete {
		return nil
	}

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found on PATH: %w", err)
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return fmt.Errorf("ffprobe not found on PATH: %w", err)
	}

	videoCodec, audioCodec, err := probeCodecs(ffprobePath, src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(cfg.HLSDir, 0o755); err != nil {
		return fmt.Errorf("create HLS root %q: %w", cfg.HLSDir, err)
	}
	tmpDir, err := os.MkdirTemp(cfg.HLSDir, "."+filepath.Base(videoName)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary HLS output dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	args := ffmpegArgs(src, tmpDir, videoCodec, audioCodec)
	cmd := exec.Command(ffmpegPath, args...)
	stderr, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg segment %q failed: %w\n%s", src, err, stderr)
	}

	tmpPlaylist := filepath.Join(tmpDir, "playlist.m3u8")
	complete, err = playlistExists(tmpPlaylist)
	if err != nil {
		return err
	}
	if !complete {
		return fmt.Errorf("ffmpeg completed but did not create non-empty playlist %q", tmpPlaylist)
	}

	if err := os.RemoveAll(outDir); err != nil {
		return fmt.Errorf("remove incomplete HLS output dir %q: %w", outDir, err)
	}
	if err := os.Rename(tmpDir, outDir); err != nil {
		return fmt.Errorf("publish HLS output %q: %w", outDir, err)
	}

	return nil
}

// SegmentAll generates HLS output for every top-level MKV in cfg.VideoDir.
func SegmentAll(cfg config.Config) error {
	if err := cleanupStaleTempDirs(cfg.HLSDir); err != nil {
		return err
	}

	entries, err := os.ReadDir(cfg.VideoDir)
	if err != nil {
		return fmt.Errorf("read video dir %q: %w", cfg.VideoDir, err)
	}

	var errs []error
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".mkv" {
			continue
		}
		videoName := strings.TrimSuffix(entry.Name(), ".mkv")
		if err := Segment(cfg, videoName); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", videoName, err))
		}
	}

	return errors.Join(errs...)
}

func cleanupStaleTempDirs(hlsDir string) error {
	entries, err := os.ReadDir(hlsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read HLS dir %q: %w", hlsDir, err)
	}

	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".tmp") {
			continue
		}
		if err := os.RemoveAll(filepath.Join(hlsDir, name)); err != nil {
			errs = append(errs, fmt.Errorf("remove stale temporary HLS dir %q: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
}

type ffprobeStream struct {
	CodecName string `json:"codec_name"`
	CodecType string `json:"codec_type"`
}

func probeCodecs(ffprobePath, src string) (string, string, error) {
	cmd := exec.Command(ffprobePath, "-v", "error", "-print_format", "json", "-show_streams", src)
	output, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("ffprobe %q failed: %w", src, err)
	}

	var result ffprobeOutput
	if err := json.Unmarshal(output, &result); err != nil {
		return "", "", fmt.Errorf("parse ffprobe output for %q: %w", src, err)
	}

	var videoCodec, audioCodec string
	for _, stream := range result.Streams {
		switch stream.CodecType {
		case "video":
			if videoCodec == "" {
				videoCodec = stream.CodecName
			}
		case "audio":
			if audioCodec == "" {
				audioCodec = stream.CodecName
			}
		}
	}
	if videoCodec == "" {
		return "", "", fmt.Errorf("no video stream found in %q", src)
	}
	if audioCodec == "" {
		return "", "", fmt.Errorf("no audio stream found in %q", src)
	}

	return videoCodec, audioCodec, nil
}

func ffmpegArgs(src, outDir, videoCodec, audioCodec string) []string {
	playlist := filepath.Join(outDir, "playlist.m3u8")
	segmentPattern := filepath.Join(outDir, "seg_%04d.ts")

	args := []string{"-y", "-i", src, "-map", "0:v:0", "-map", "0:a:0"}
	if videoCodec == "h264" {
		args = append(args, "-c:v", "copy")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "22", "-sc_threshold", "0", "-g", "144", "-keyint_min", "144")
	}
	if audioCodec == "aac" {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, "-c:a", "aac", "-b:a", "192k", "-ac", "2")
	}

	return append(args,
		"-f", "hls",
		"-hls_time", "6",
		"-hls_playlist_type", "vod",
		"-hls_flags", "independent_segments",
		"-hls_segment_filename", segmentPattern,
		playlist,
	)
}

func playlistExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("stat playlist %q: %w", path, err)
	}

	return !info.IsDir() && info.Size() > 0, nil
}
