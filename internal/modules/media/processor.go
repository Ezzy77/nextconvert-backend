package media

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/convert-studio/backend/internal/shared/storage"
	"go.uber.org/zap"
)

// Processor handles FFmpeg operations
type Processor struct {
	storage    *storage.Service
	ffmpegPath string
	logger     *zap.Logger
}

// ProcessOptions contains options for media processing
type ProcessOptions struct {
	InputPath  string
	OutputPath string
	Operations []Operation
	OnProgress func(percent int, operation string)
}

// NewProcessor creates a new media processor
func NewProcessor(storage *storage.Service, ffmpegPath string, logger *zap.Logger) *Processor {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return &Processor{
		storage:    storage,
		ffmpegPath: ffmpegPath,
		logger:     logger,
	}
}

// Process executes media operations
func (p *Processor) Process(ctx context.Context, opts ProcessOptions) error {
	// Build FFmpeg command
	args := p.buildFFmpegArgs(opts)

	p.logger.Info("Executing FFmpeg",
		zap.String("input", opts.InputPath),
		zap.String("output", opts.OutputPath),
		zap.Strings("args", args),
	)

	cmd := exec.CommandContext(ctx, p.ffmpegPath, args...)

	// Capture stderr for progress
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start FFmpeg: %w", err)
	}

	// Parse progress from stderr
	go p.parseProgress(stderr, opts.OnProgress)

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("FFmpeg execution failed: %w", err)
	}

	return nil
}

func (p *Processor) buildFFmpegArgs(opts ProcessOptions) []string {
	args := []string{"-y", "-i", opts.InputPath}

	var videoFilters []string
	var audioFilters []string

	for _, op := range opts.Operations {
		switch op.Type {
		case "trim":
			if start, ok := op.Params["startTime"].(string); ok {
				args = append(args, "-ss", start)
			}
			if end, ok := op.Params["endTime"].(string); ok {
				args = append(args, "-to", end)
			}

		case "resize":
			width := getIntParam(op.Params, "width", 0)
			height := getIntParam(op.Params, "height", 0)
			maintainAspect := getBoolParam(op.Params, "maintainAspect", true)

			if width > 0 || height > 0 {
				if maintainAspect {
					if width > 0 && height > 0 {
						videoFilters = append(videoFilters, fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease", width, height))
					} else if width > 0 {
						videoFilters = append(videoFilters, fmt.Sprintf("scale=%d:-2", width))
					} else {
						videoFilters = append(videoFilters, fmt.Sprintf("scale=-2:%d", height))
					}
				} else {
					videoFilters = append(videoFilters, fmt.Sprintf("scale=%d:%d", width, height))
				}
			}

		case "compress":
			quality := getIntParam(op.Params, "quality", 70)
			// Convert quality (1-100) to CRF (0-51, lower is better)
			crf := 51 - (quality * 51 / 100)
			args = append(args, "-crf", strconv.Itoa(crf))

		case "convertFormat":
			if codec, ok := op.Params["codec"].(string); ok {
				switch codec {
				case "h264":
					args = append(args, "-c:v", "libx264", "-preset", "medium")
				case "h265":
					args = append(args, "-c:v", "libx265", "-preset", "medium")
				case "vp9":
					args = append(args, "-c:v", "libvpx-vp9")
				}
			}

		case "rotate":
			degrees := getIntParam(op.Params, "degrees", 0)
			switch degrees {
			case 90:
				videoFilters = append(videoFilters, "transpose=1")
			case 180:
				videoFilters = append(videoFilters, "transpose=1,transpose=1")
			case 270:
				videoFilters = append(videoFilters, "transpose=2")
			}

		case "crop":
			x := getIntParam(op.Params, "x", 0)
			y := getIntParam(op.Params, "y", 0)
			w := getIntParam(op.Params, "width", 0)
			h := getIntParam(op.Params, "height", 0)
			if w > 0 && h > 0 {
				videoFilters = append(videoFilters, fmt.Sprintf("crop=%d:%d:%d:%d", w, h, x, y))
			}

		case "extractAudio":
			args = append(args, "-vn")
			if format, ok := op.Params["format"].(string); ok {
				switch format {
				case "mp3":
					args = append(args, "-acodec", "libmp3lame")
				case "aac":
					args = append(args, "-acodec", "aac")
				}
			}

		case "changeSpeed":
			multiplier := getFloatParam(op.Params, "multiplier", 1.0)
			if multiplier != 1.0 {
				videoFilters = append(videoFilters, fmt.Sprintf("setpts=%.2f*PTS", 1/multiplier))
				audioFilters = append(audioFilters, fmt.Sprintf("atempo=%.2f", multiplier))
			}

		case "createGif":
			fps := getIntParam(op.Params, "fps", 10)
			width := getIntParam(op.Params, "width", 480)
			videoFilters = append(videoFilters, fmt.Sprintf("fps=%d,scale=%d:-1:flags=lanczos", fps, width))

		case "changeBitrate":
			bitrate := getIntParam(op.Params, "bitrate", 128000)
			args = append(args, "-b:a", fmt.Sprintf("%d", bitrate))

		case "adjustVolume":
			db := getFloatParam(op.Params, "db", 0)
			if db != 0 {
				audioFilters = append(audioFilters, fmt.Sprintf("volume=%.1fdB", db))
			}

		case "fadeInOut":
			fadeIn := getFloatParam(op.Params, "fadeIn", 0)
			fadeOut := getFloatParam(op.Params, "fadeOut", 0)
			if fadeIn > 0 {
				audioFilters = append(audioFilters, fmt.Sprintf("afade=t=in:st=0:d=%.1f", fadeIn))
			}
			if fadeOut > 0 {
				audioFilters = append(audioFilters, fmt.Sprintf("afade=t=out:st=0:d=%.1f", fadeOut))
			}
		}
	}

	// Apply video filters
	if len(videoFilters) > 0 {
		args = append(args, "-vf", strings.Join(videoFilters, ","))
	}

	// Apply audio filters
	if len(audioFilters) > 0 {
		args = append(args, "-af", strings.Join(audioFilters, ","))
	}

	args = append(args, opts.OutputPath)
	return args
}

func (p *Processor) parseProgress(stderr interface{ Read([]byte) (int, error) }, onProgress func(int, string)) {
	if onProgress == nil {
		return
	}

	scanner := bufio.NewScanner(stderr)
	progressRegex := regexp.MustCompile(`time=(\d+):(\d+):(\d+)\.(\d+)`)

	for scanner.Scan() {
		line := scanner.Text()
		matches := progressRegex.FindStringSubmatch(line)
		if len(matches) > 0 {
			// Parse time and calculate progress
			// This is a simplified version; real implementation would need total duration
			hours, _ := strconv.Atoi(matches[1])
			minutes, _ := strconv.Atoi(matches[2])
			seconds, _ := strconv.Atoi(matches[3])
			totalSeconds := hours*3600 + minutes*60 + seconds
			_ = totalSeconds // Use for progress calculation
		}
	}
}

// Probe extracts metadata using ffprobe
func (p *Processor) Probe(ctx context.Context, inputPath string) (*MediaInfo, error) {
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		inputPath,
	}

	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	// Parse JSON output
	// For now, return a placeholder
	_ = output

	return &MediaInfo{
		Format: "mp4",
	}, nil
}

// GenerateThumbnail creates a thumbnail from a video
func (p *Processor) GenerateThumbnail(ctx context.Context, inputPath, outputPath string, timestamp float64) error {
	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.2f", timestamp),
		"-i", inputPath,
		"-vframes", "1",
		"-vf", "scale=320:-1",
		outputPath,
	}

	cmd := exec.CommandContext(ctx, p.ffmpegPath, args...)
	return cmd.Run()
}

// Helper functions
func getIntParam(params map[string]interface{}, key string, defaultVal int) int {
	if v, ok := params[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case float64:
			return int(val)
		case string:
			if i, err := strconv.Atoi(val); err == nil {
				return i
			}
		}
	}
	return defaultVal
}

func getFloatParam(params map[string]interface{}, key string, defaultVal float64) float64 {
	if v, ok := params[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		case string:
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				return f
			}
		}
	}
	return defaultVal
}

func getBoolParam(params map[string]interface{}, key string, defaultVal bool) bool {
	if v, ok := params[key]; ok {
		switch val := v.(type) {
		case bool:
			return val
		case string:
			return val == "true" || val == "1"
		}
	}
	return defaultVal
}
