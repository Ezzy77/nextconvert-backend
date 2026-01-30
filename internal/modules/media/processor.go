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
	storage           *storage.Service
	ffmpegPath        string
	logger            *zap.Logger
	maxThreads        int  // Limit CPU threads (0 = auto/unlimited)
	useHardwareAccel  bool // Use hardware acceleration when available
	preferFastPresets bool // Use faster presets to reduce CPU load
}

// ProcessorConfig configures processor behavior
type ProcessorConfig struct {
	FFmpegPath        string
	MaxThreads        int  // 0 = unlimited, recommended: 2-4 for background processing
	UseHardwareAccel  bool // Use VideoToolbox on macOS, NVENC on Linux/Windows
	PreferFastPresets bool // Use "veryfast" instead of "medium" preset
}

// ProcessOptions contains options for media processing
type ProcessOptions struct {
	InputPath  string
	InputPaths []string // For merge operations with multiple inputs
	OutputPath string
	Operations []Operation
	OnProgress func(percent int, operation string)
}

// NewProcessor creates a new media processor with cloud-friendly defaults
func NewProcessor(storage *storage.Service, ffmpegPath string, logger *zap.Logger) *Processor {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return &Processor{
		storage:           storage,
		ffmpegPath:        ffmpegPath,
		logger:            logger,
		maxThreads:        0,     // Default: 0 = auto (use available cores)
		useHardwareAccel:  false, // Default: disabled for cloud servers
		preferFastPresets: true,  // Default: use faster presets
	}
}

// NewProcessorWithConfig creates a processor with custom configuration
func NewProcessorWithConfig(storage *storage.Service, config ProcessorConfig, logger *zap.Logger) *Processor {
	if config.FFmpegPath == "" {
		config.FFmpegPath = "ffmpeg"
	}
	return &Processor{
		storage:           storage,
		ffmpegPath:        config.FFmpegPath,
		logger:            logger,
		maxThreads:        config.MaxThreads,
		useHardwareAccel:  config.UseHardwareAccel,
		preferFastPresets: config.PreferFastPresets,
	}
}

// Process executes media operations
func (p *Processor) Process(ctx context.Context, opts ProcessOptions) error {
	// Check if this is a merge operation
	for _, op := range opts.Operations {
		if op.Type == "merge" {
			return p.processMerge(ctx, opts)
		}
	}

	// Build FFmpeg command for standard operations
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

// processMerge handles video merging using FFmpeg concat demuxer
// MergeOptions contains options for merging videos
type MergeOptions struct {
	InputPaths []string
	OutputPath string
	OnProgress func(percent int, operation string)
}

// ProcessMerge merges multiple videos into one (public interface method)
func (p *Processor) ProcessMerge(ctx context.Context, opts MergeOptions) error {
	return p.processMerge(ctx, ProcessOptions{
		InputPaths: opts.InputPaths,
		OutputPath: opts.OutputPath,
		OnProgress: opts.OnProgress,
	})
}

func (p *Processor) processMerge(ctx context.Context, opts ProcessOptions) error {
	// Collect all input paths
	inputPaths := opts.InputPaths
	if len(inputPaths) == 0 && opts.InputPath != "" {
		inputPaths = []string{opts.InputPath}
	}

	if len(inputPaths) < 2 {
		return fmt.Errorf("merge requires at least 2 input files")
	}

	p.logger.Info("Merging videos",
		zap.Int("count", len(inputPaths)),
		zap.Strings("inputs", inputPaths),
		zap.String("output", opts.OutputPath),
	)

	// Use filter_complex concat to handle videos with different codecs/resolutions
	// This re-encodes everything to ensure compatibility
	args := []string{"-y"}

	if p.maxThreads > 0 {
		args = append(args, "-threads", strconv.Itoa(p.maxThreads))
	}

	// Add all input files
	for _, inputPath := range inputPaths {
		args = append(args, "-i", inputPath)
	}

	// Build the filter_complex string for concat
	// First, scale all videos to the same resolution (use first video's resolution or 1080p)
	// and normalize audio to stereo 44.1kHz
	var filterParts []string
	for i := range inputPaths {
		// Scale video to 1920x1080, pad if needed to maintain aspect ratio
		// Also normalize pixel format to yuv420p for compatibility
		filterParts = append(filterParts,
			fmt.Sprintf("[%d:v]scale=1920:1080:force_original_aspect_ratio=decrease,pad=1920:1080:(ow-iw)/2:(oh-ih)/2,setsar=1,fps=30,format=yuv420p[v%d]", i, i),
		)
		// Normalize audio to stereo 44.1kHz
		filterParts = append(filterParts,
			fmt.Sprintf("[%d:a]aformat=sample_fmts=fltp:sample_rates=44100:channel_layouts=stereo[a%d]", i, i),
		)
	}

	// Build concat input string
	var concatInputs string
	for i := range inputPaths {
		concatInputs += fmt.Sprintf("[v%d][a%d]", i, i)
	}

	// Add the concat filter
	filterParts = append(filterParts,
		fmt.Sprintf("%sconcat=n=%d:v=1:a=1[outv][outa]", concatInputs, len(inputPaths)),
	)

	filterComplex := strings.Join(filterParts, ";")

	args = append(args, "-filter_complex", filterComplex)
	args = append(args, "-map", "[outv]", "-map", "[outa]")

	// Encode with H.264 and AAC for maximum compatibility
	preset := "medium"
	if p.preferFastPresets {
		preset = "veryfast"
	}

	if p.useHardwareAccel {
		args = append(args, "-c:v", "h264_videotoolbox", "-b:v", "5M")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", preset, "-crf", "23")
	}
	args = append(args, "-c:a", "aac", "-b:a", "192k")

	args = append(args, opts.OutputPath)

	p.logger.Info("Executing FFmpeg merge with re-encoding",
		zap.Strings("args", args),
	)

	cmd := exec.CommandContext(ctx, p.ffmpegPath, args...)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start FFmpeg: %w", err)
	}

	go p.parseProgress(stderr, opts.OnProgress)

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("FFmpeg merge failed: %w", err)
	}

	return nil
}

func (p *Processor) buildFFmpegArgs(opts ProcessOptions) []string {
	args := []string{"-y"}

	// Limit CPU threads to reduce system load
	if p.maxThreads > 0 {
		args = append(args, "-threads", strconv.Itoa(p.maxThreads))
	}

	args = append(args, "-i", opts.InputPath)

	var videoFilters []string
	var audioFilters []string

	// Determine the preset to use based on configuration
	preset := "medium"
	if p.preferFastPresets {
		preset = "veryfast" // Much faster encoding, slightly larger file size
	}

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
			// Add codec with fast preset for compression
			if p.useHardwareAccel {
				// Try macOS VideoToolbox hardware encoder (very fast, low CPU)
				args = append(args, "-c:v", "h264_videotoolbox", "-c:a", "aac")
			} else {
				args = append(args, "-c:v", "libx264", "-preset", preset, "-c:a", "aac")
			}

		case "convertFormat":
			// Handle targetFormat (mp4, webm, mov, avi, mkv) from frontend
			if targetFormat, ok := op.Params["targetFormat"].(string); ok {
				switch targetFormat {
				case "mp4", "mov":
					if p.useHardwareAccel {
						// macOS VideoToolbox - hardware H.264 encoder (minimal CPU usage)
						args = append(args, "-c:v", "h264_videotoolbox", "-c:a", "aac")
					} else {
						args = append(args, "-c:v", "libx264", "-preset", preset, "-c:a", "aac")
					}
				case "webm":
					// VP9 has no hardware encoder on macOS, but we can limit CPU usage
					// by using lower quality settings and row-mt for better threading
					args = append(args, "-c:v", "libvpx-vp9", "-cpu-used", "4", "-row-mt", "1", "-c:a", "libopus")
				case "avi":
					// MPEG-4 for AVI format
					args = append(args, "-c:v", "mpeg4", "-c:a", "mp3")
				case "mkv":
					if p.useHardwareAccel {
						args = append(args, "-c:v", "h264_videotoolbox", "-c:a", "aac")
					} else {
						args = append(args, "-c:v", "libx264", "-preset", preset, "-c:a", "aac")
					}
				}
			} else if codec, ok := op.Params["codec"].(string); ok {
				// Backward compatibility: handle codec param directly
				switch codec {
				case "h264":
					if p.useHardwareAccel {
						args = append(args, "-c:v", "h264_videotoolbox")
					} else {
						args = append(args, "-c:v", "libx264", "-preset", preset)
					}
				case "h265":
					if p.useHardwareAccel {
						// macOS VideoToolbox HEVC encoder
						args = append(args, "-c:v", "hevc_videotoolbox")
					} else {
						args = append(args, "-c:v", "libx265", "-preset", preset)
					}
				case "vp9":
					args = append(args, "-c:v", "libvpx-vp9", "-cpu-used", "4", "-row-mt", "1")
				}
			}

		case "rotate":
			degrees := getIntParam(op.Params, "degrees", 0)
			flipH := getBoolParam(op.Params, "flipHorizontal", false)
			flipV := getBoolParam(op.Params, "flipVertical", false)

			// Apply rotation
			switch degrees {
			case 90:
				videoFilters = append(videoFilters, "transpose=1")
			case 180:
				videoFilters = append(videoFilters, "transpose=1,transpose=1")
			case 270:
				videoFilters = append(videoFilters, "transpose=2")
			}

			// Apply flips
			if flipH {
				videoFilters = append(videoFilters, "hflip")
			}
			if flipV {
				videoFilters = append(videoFilters, "vflip")
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
			args = append(args, "-vn") // Remove video stream
			format := getStringParam(op.Params, "format", "mp3")
			bitrate := getIntParam(op.Params, "bitrate", 192000)

			switch format {
			case "mp3":
				args = append(args, "-acodec", "libmp3lame")
				args = append(args, "-b:a", fmt.Sprintf("%dk", bitrate/1000))
			case "aac":
				args = append(args, "-acodec", "aac")
				args = append(args, "-b:a", fmt.Sprintf("%dk", bitrate/1000))
			case "wav":
				args = append(args, "-acodec", "pcm_s16le")
				// WAV doesn't use bitrate compression
			case "flac":
				args = append(args, "-acodec", "flac")
				// FLAC is lossless, compression level instead of bitrate
				args = append(args, "-compression_level", "5")
			case "ogg":
				args = append(args, "-acodec", "libvorbis")
				args = append(args, "-b:a", fmt.Sprintf("%dk", bitrate/1000))
			default:
				args = append(args, "-acodec", "libmp3lame")
				args = append(args, "-b:a", "192k")
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

		case "addWatermark":
			// Text watermark support
			if text, ok := op.Params["text"].(string); ok && text != "" {
				position := getStringParam(op.Params, "position", "bottomright")
				fontSize := getIntParam(op.Params, "fontSize", 24)
				fontColor := getStringParam(op.Params, "fontColor", "white")
				opacity := getFloatParam(op.Params, "opacity", 0.8)

				// Map position to FFmpeg coordinates
				var x, y string
				switch position {
				case "topleft":
					x, y = "10", "10"
				case "topright":
					x, y = "w-tw-10", "10"
				case "bottomleft":
					x, y = "10", "h-th-10"
				case "bottomright":
					x, y = "w-tw-10", "h-th-10"
				case "center":
					x, y = "(w-tw)/2", "(h-th)/2"
				default:
					x, y = "w-tw-10", "h-th-10" // default to bottom right
				}

				// Escape special characters for FFmpeg drawtext filter
				// Colons and backslashes need escaping
				escapedText := text
				escapedText = strings.ReplaceAll(escapedText, "\\", "\\\\")
				escapedText = strings.ReplaceAll(escapedText, "'", "'\\''")
				escapedText = strings.ReplaceAll(escapedText, ":", "\\:")

				// Build drawtext filter with font file
				// Use DejaVu Sans which is installed in the Docker container
				filter := fmt.Sprintf(
					"drawtext=text='%s':fontfile=/usr/share/fonts/ttf-dejavu/DejaVuSans.ttf:fontsize=%d:fontcolor=%s:alpha=%.2f:x=%s:y=%s",
					escapedText,
					fontSize,
					fontColor,
					opacity,
					x, y,
				)
				videoFilters = append(videoFilters, filter)
			}

		case "filters":
			// Video filter adjustments using FFmpeg eq filter
			brightness := getFloatParam(op.Params, "brightness", 0)  // -1 to 1 (0 = no change)
			contrast := getFloatParam(op.Params, "contrast", 1)      // 0 to 2 (1 = no change)
			saturation := getFloatParam(op.Params, "saturation", 1)  // 0 to 3 (1 = no change)
			gamma := getFloatParam(op.Params, "gamma", 1)            // 0.1 to 10 (1 = no change)
			hue := getFloatParam(op.Params, "hue", 0)                // -180 to 180 degrees (0 = no change)
			blur := getFloatParam(op.Params, "blur", 0)              // 0 to 10 (0 = no blur)
			sharpen := getFloatParam(op.Params, "sharpen", 0)        // 0 to 5 (0 = no sharpen)
			vignette := getBoolParam(op.Params, "vignette", false)   // Add vignette effect
			grayscale := getBoolParam(op.Params, "grayscale", false) // Convert to grayscale
			sepia := getBoolParam(op.Params, "sepia", false)         // Apply sepia tone
			negative := getBoolParam(op.Params, "negative", false)   // Invert colors

			// Build eq filter for brightness, contrast, saturation, gamma
			eqParts := []string{}
			if brightness != 0 {
				eqParts = append(eqParts, fmt.Sprintf("brightness=%.2f", brightness))
			}
			if contrast != 1 {
				eqParts = append(eqParts, fmt.Sprintf("contrast=%.2f", contrast))
			}
			if saturation != 1 {
				eqParts = append(eqParts, fmt.Sprintf("saturation=%.2f", saturation))
			}
			if gamma != 1 {
				eqParts = append(eqParts, fmt.Sprintf("gamma=%.2f", gamma))
			}
			if len(eqParts) > 0 {
				videoFilters = append(videoFilters, "eq="+strings.Join(eqParts, ":"))
			}

			// Hue adjustment
			if hue != 0 {
				videoFilters = append(videoFilters, fmt.Sprintf("hue=h=%.1f", hue))
			}

			// Blur effect (using boxblur)
			if blur > 0 {
				blurRadius := int(blur * 2) // Scale for more visible effect
				if blurRadius < 1 {
					blurRadius = 1
				}
				videoFilters = append(videoFilters, fmt.Sprintf("boxblur=%d:%d", blurRadius, blurRadius))
			}

			// Sharpen effect (using unsharp mask)
			if sharpen > 0 {
				// unsharp=lx:ly:la where l=luma, a=amount
				amount := sharpen * 1.5 // Scale for better effect
				videoFilters = append(videoFilters, fmt.Sprintf("unsharp=5:5:%.1f:5:5:0", amount))
			}

			// Vignette effect
			if vignette {
				videoFilters = append(videoFilters, "vignette=PI/4")
			}

			// Grayscale
			if grayscale {
				videoFilters = append(videoFilters, "colorchannelmixer=.3:.4:.3:0:.3:.4:.3:0:.3:.4:.3")
			}

			// Sepia tone (apply after grayscale-like transform)
			if sepia {
				videoFilters = append(videoFilters, "colorchannelmixer=.393:.769:.189:0:.349:.686:.168:0:.272:.534:.131")
			}

			// Negative/Invert colors
			if negative {
				videoFilters = append(videoFilters, "negate")
			}

		case "split":
			// Split is handled separately in processSplit method
			// This case is here to avoid "unknown operation" errors
			// The actual split logic extracts segments at specific times

		case "thumbnail":
			// Thumbnail generation - extract frame(s) as image(s)
			// This is handled specially since output is image not video
			timestamp := getStringParam(op.Params, "timestamp", "00:00:01")
			width := getIntParam(op.Params, "width", 320)

			// For thumbnail, we override the entire args since it's different
			args = []string{"-ss", timestamp, "-i", opts.InputPath}
			args = append(args, "-vframes", "1")
			args = append(args, "-vf", fmt.Sprintf("scale=%d:-1", width))
			args = append(args, "-q:v", "2") // High quality JPEG
			args = append(args, opts.OutputPath)
			return args // Return early for thumbnail

		case "addAudio":
			// Add/replace audio track
			// This requires a second input file (audio)
			audioPath := getStringParam(op.Params, "audioPath", "")
			mode := getStringParam(op.Params, "mode", "mix") // mix, replace
			volume := getFloatParam(op.Params, "volume", 1.0)

			if audioPath != "" {
				// We need to handle this differently with filter_complex
				// This will be processed specially
				if mode == "replace" {
					// Remove original audio, use new audio
					args = []string{"-i", opts.InputPath, "-i", audioPath}
					args = append(args, "-map", "0:v", "-map", "1:a")
					args = append(args, "-c:v", "copy")
					if volume != 1.0 {
						args = append(args, "-af", fmt.Sprintf("volume=%.2f", volume))
					}
					args = append(args, "-shortest")
					args = append(args, opts.OutputPath)
					return args
				} else {
					// Mix both audio tracks
					args = []string{"-i", opts.InputPath, "-i", audioPath}
					volumeFilter := ""
					if volume != 1.0 {
						volumeFilter = fmt.Sprintf(",volume=%.2f", volume)
					}
					args = append(args, "-filter_complex", fmt.Sprintf("[0:a][1:a]amix=inputs=2:duration=first%s[aout]", volumeFilter))
					args = append(args, "-map", "0:v", "-map", "[aout]")
					args = append(args, "-c:v", "copy")
					args = append(args, opts.OutputPath)
					return args
				}
			}

		case "addText":
			// Add text overlay (similar to watermark but with more options)
			text := getStringParam(op.Params, "text", "")
			if text != "" {
				position := getStringParam(op.Params, "position", "center")
				fontSize := getIntParam(op.Params, "fontSize", 48)
				fontColor := getStringParam(op.Params, "fontColor", "white")
				bgColor := getStringParam(op.Params, "bgColor", "")
				bgOpacity := getFloatParam(op.Params, "bgOpacity", 0.5)
				startTime := getFloatParam(op.Params, "startTime", 0)
				endTime := getFloatParam(op.Params, "endTime", 0) // 0 means entire video
				animation := getStringParam(op.Params, "animation", "none")

				// Map position to coordinates
				var x, y string
				switch position {
				case "topleft":
					x, y = "20", "20"
				case "topcenter":
					x, y = "(w-tw)/2", "20"
				case "topright":
					x, y = "w-tw-20", "20"
				case "centerleft":
					x, y = "20", "(h-th)/2"
				case "center":
					x, y = "(w-tw)/2", "(h-th)/2"
				case "centerright":
					x, y = "w-tw-20", "(h-th)/2"
				case "bottomleft":
					x, y = "20", "h-th-20"
				case "bottomcenter":
					x, y = "(w-tw)/2", "h-th-20"
				case "bottomright":
					x, y = "w-tw-20", "h-th-20"
				default:
					x, y = "(w-tw)/2", "(h-th)/2"
				}

				// Apply animation to position
				switch animation {
				case "scrollLeft":
					x = fmt.Sprintf("w-%d*t", fontSize*2) // Scroll from right to left
				case "scrollRight":
					x = fmt.Sprintf("-%d+%d*t", fontSize*5, fontSize*2) // Scroll from left to right
				case "scrollUp":
					y = fmt.Sprintf("h-%d*t", fontSize) // Scroll from bottom to top
				case "scrollDown":
					y = fmt.Sprintf("-%d+%d*t", fontSize*2, fontSize) // Scroll from top to bottom
				case "fadeIn":
					// Fade handled via alpha
				}

				// Escape special characters
				escapedText := text
				escapedText = strings.ReplaceAll(escapedText, "\\", "\\\\")
				escapedText = strings.ReplaceAll(escapedText, "'", "'\\''")
				escapedText = strings.ReplaceAll(escapedText, ":", "\\:")

				// Build filter
				filter := fmt.Sprintf(
					"drawtext=text='%s':fontfile=/usr/share/fonts/ttf-dejavu/DejaVuSans-Bold.ttf:fontsize=%d:fontcolor=%s:x=%s:y=%s",
					escapedText, fontSize, fontColor, x, y,
				)

				// Add background box if specified
				if bgColor != "" {
					filter += fmt.Sprintf(":box=1:boxcolor=%s@%.2f:boxborderw=10", bgColor, bgOpacity)
				}

				// Add time constraints
				if startTime > 0 || endTime > 0 {
					if endTime > 0 {
						filter += fmt.Sprintf(":enable='between(t,%.2f,%.2f)'", startTime, endTime)
					} else {
						filter += fmt.Sprintf(":enable='gte(t,%.2f)'", startTime)
					}
				}

				// Add fade animation
				if animation == "fadeIn" {
					filter += ":alpha='if(lt(t,1),t,1)'"
				}

				videoFilters = append(videoFilters, filter)
			}

		case "removeAudio":
			// Strip audio track from video
			args = append(args, "-an")

		case "reverse":
			// Reverse video playback
			videoFilters = append(videoFilters, "reverse")
			audioFilters = append(audioFilters, "areverse")

		case "loop":
			// Loop video N times
			loopCount := getIntParam(op.Params, "count", 2)
			args = append(args, "-stream_loop", fmt.Sprintf("%d", loopCount-1))
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

	// Check if we need to add default codec settings for operations that don't set them
	// This prevents FFmpeg from using slow default settings
	hasVideoCodec := false
	for _, arg := range args {
		if arg == "-c:v" || strings.HasPrefix(arg, "-codec:v") {
			hasVideoCodec = true
			break
		}
	}

	// If no video codec was specified, add optimized default
	if !hasVideoCodec && len(videoFilters) > 0 {
		if p.useHardwareAccel {
			args = append(args, "-c:v", "h264_videotoolbox", "-c:a", "aac")
		} else {
			preset := "medium"
			if p.preferFastPresets {
				preset = "veryfast"
			}
			args = append(args, "-c:v", "libx264", "-preset", preset, "-c:a", "aac")
		}
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

func getStringParam(params map[string]interface{}, key string, defaultVal string) string {
	if v, ok := params[key]; ok {
		if str, ok := v.(string); ok {
			return str
		}
	}
	return defaultVal
}
