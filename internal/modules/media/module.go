package media

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/nextconvert/backend/internal/modules/jobs"
	"github.com/nextconvert/backend/internal/shared/database"
	"github.com/nextconvert/backend/internal/shared/storage"
	"go.uber.org/zap"
)

// Module handles media operations
type Module struct {
	db       *database.Postgres
	storage  *storage.Service
	jobQueue *jobs.QueueClient
	logger   *zap.Logger
	presets  map[string]Preset
}

// Operation represents a media operation
type Operation struct {
	Type   string                 `json:"type"`
	Params map[string]interface{} `json:"params"`
}

// MediaInfo contains metadata about a media file
type MediaInfo struct {
	Format     string       `json:"format"`
	Duration   float64      `json:"duration"`
	Size       int64        `json:"size"`
	BitRate    int          `json:"bitRate"`
	VideoCodec string       `json:"videoCodec,omitempty"`
	AudioCodec string       `json:"audioCodec,omitempty"`
	Width      int          `json:"width,omitempty"`
	Height     int          `json:"height,omitempty"`
	FrameRate  float64      `json:"frameRate,omitempty"`
	Streams    []StreamInfo `json:"streams"`
}

// StreamInfo contains information about a media stream
type StreamInfo struct {
	Index      int    `json:"index"`
	Type       string `json:"type"`
	Codec      string `json:"codec"`
	BitRate    int    `json:"bitRate,omitempty"`
	Channels   int    `json:"channels,omitempty"`
	SampleRate int    `json:"sampleRate,omitempty"`
}

// Preset represents a predefined operation set
type Preset struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Operations  []Operation `json:"operations"`
	Icon        string      `json:"icon,omitempty"`
}

// ValidationResult contains operation validation results
type ValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// FormatInfo describes a supported format
type FormatInfo struct {
	Name      string   `json:"name"`
	Extension string   `json:"extension"`
	MimeTypes []string `json:"mimeTypes"`
	Type      string   `json:"type"` // video, audio, image
	Encodable bool     `json:"encodable"`
	Decodable bool     `json:"decodable"`
}

// CodecInfo describes an available codec
type CodecInfo struct {
	Name     string `json:"name"`
	LongName string `json:"longName"`
	Type     string `json:"type"` // video, audio
	Encoding bool   `json:"encoding"`
	Decoding bool   `json:"decoding"`
}

// NewModule creates a new media module
func NewModule(db *database.Postgres, storage *storage.Service, jobQueue *jobs.QueueClient, logger *zap.Logger) *Module {
	m := &Module{
		db:       db,
		storage:  storage,
		jobQueue: jobQueue,
		logger:   logger,
		presets:  make(map[string]Preset),
	}

	m.initPresets()
	return m
}

func (m *Module) initPresets() {
	// Mobile optimized preset
	m.presets["mobile"] = Preset{
		ID:          "mobile",
		Name:        "Mobile Optimized",
		Type:        "video",
		Description: "Optimized for mobile devices (720p, H.264)",
		Icon:        "📱",
		Operations: []Operation{
			{Type: "resize", Params: map[string]interface{}{"width": 1280, "height": 720, "maintainAspect": true}},
			{Type: "compress", Params: map[string]interface{}{"quality": 70}},
			{Type: "convertFormat", Params: map[string]interface{}{"targetFormat": "mp4", "codec": "h264"}},
		},
	}

	// Web optimized preset
	m.presets["web"] = Preset{
		ID:          "web",
		Name:        "Web Optimized",
		Type:        "video",
		Description: "Optimized for web streaming (1080p, WebM)",
		Icon:        "🌐",
		Operations: []Operation{
			{Type: "resize", Params: map[string]interface{}{"width": 1920, "height": 1080, "maintainAspect": true}},
			{Type: "compress", Params: map[string]interface{}{"quality": 80}},
			{Type: "convertFormat", Params: map[string]interface{}{"targetFormat": "webm", "codec": "vp9"}},
		},
	}

	// Email attachment preset
	m.presets["email"] = Preset{
		ID:          "email",
		Name:        "Email Attachment",
		Type:        "video",
		Description: "Small file size for email (<25MB target)",
		Icon:        "📧",
		Operations: []Operation{
			{Type: "resize", Params: map[string]interface{}{"width": 640, "height": 480, "maintainAspect": true}},
			{Type: "compress", Params: map[string]interface{}{"targetSize": 25000000}},
			{Type: "convertFormat", Params: map[string]interface{}{"targetFormat": "mp4", "codec": "h264"}},
		},
	}

	// Audio podcast preset
	m.presets["podcast"] = Preset{
		ID:          "podcast",
		Name:        "Podcast Audio",
		Type:        "audio",
		Description: "Optimized for podcast distribution (MP3, 128kbps)",
		Icon:        "🎙️",
		Operations: []Operation{
			{Type: "convertFormat", Params: map[string]interface{}{"targetFormat": "mp3"}},
			{Type: "changeBitrate", Params: map[string]interface{}{"bitrate": 128000}},
		},
	}

	// GIF creation preset
	m.presets["gif"] = Preset{
		ID:          "gif",
		Name:        "Create GIF",
		Type:        "video",
		Description: "Convert video clip to animated GIF",
		Icon:        "🎬",
		Operations: []Operation{
			{Type: "createGif", Params: map[string]interface{}{"fps": 10, "width": 480}},
		},
	}
}

// ffprobeOutput represents the JSON output from ffprobe
type ffprobeOutput struct {
	Format struct {
		Filename   string `json:"filename"`
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		Size       string `json:"size"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
	Streams []struct {
		Index        int    `json:"index"`
		CodecType    string `json:"codec_type"`
		CodecName    string `json:"codec_name"`
		Width        int    `json:"width,omitempty"`
		Height       int    `json:"height,omitempty"`
		RFrameRate   string `json:"r_frame_rate,omitempty"`
		AvgFrameRate string `json:"avg_frame_rate,omitempty"`
		BitRate      string `json:"bit_rate,omitempty"`
		Channels     int    `json:"channels,omitempty"`
		SampleRate   string `json:"sample_rate,omitempty"`
	} `json:"streams"`
}

// Probe extracts metadata from a media file
func (m *Module) Probe(ctx context.Context, fileID string) (*MediaInfo, error) {
	// Get file path from database
	var storagePath string
	var size int64
	err := m.db.Pool.QueryRow(ctx,
		"SELECT storage_path, size_bytes FROM files WHERE id = $1", fileID).Scan(&storagePath, &size)
	if err != nil {
		m.logger.Error("Failed to get file from database", zap.Error(err), zap.String("file_id", fileID))
		return nil, fmt.Errorf("file not found: %w", err)
	}

	// For remote storage (S3), download file to temp location first
	localPath, cleanup, err := m.storage.PrepareInputForProcessing(ctx, storagePath)
	if err != nil {
		m.logger.Error("Failed to prepare file for probing", zap.Error(err), zap.String("storage_path", storagePath))
		return nil, fmt.Errorf("failed to prepare file: %w", err)
	}
	defer cleanup()

	// Run ffprobe
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		localPath,
	}

	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	output, err := cmd.Output()
	if err != nil {
		m.logger.Error("ffprobe failed", zap.Error(err), zap.String("path", localPath))
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	// Parse JSON output
	var probeData ffprobeOutput
	if err := json.Unmarshal(output, &probeData); err != nil {
		m.logger.Error("Failed to parse ffprobe output", zap.Error(err))
		return nil, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	// Build MediaInfo
	info := &MediaInfo{
		Format:  probeData.Format.FormatName,
		Size:    size,
		Streams: make([]StreamInfo, 0),
	}

	// Parse duration
	if probeData.Format.Duration != "" {
		if d, err := strconv.ParseFloat(probeData.Format.Duration, 64); err == nil {
			info.Duration = d
		}
	}

	// Parse bitrate
	if probeData.Format.BitRate != "" {
		if br, err := strconv.Atoi(probeData.Format.BitRate); err == nil {
			info.BitRate = br
		}
	}

	// Process streams
	for _, stream := range probeData.Streams {
		streamInfo := StreamInfo{
			Index: stream.Index,
			Type:  stream.CodecType,
			Codec: stream.CodecName,
		}

		if stream.BitRate != "" {
			if br, err := strconv.Atoi(stream.BitRate); err == nil {
				streamInfo.BitRate = br
			}
		}

		if stream.CodecType == "video" {
			info.VideoCodec = stream.CodecName
			info.Width = stream.Width
			info.Height = stream.Height

			// Parse frame rate (format: "30000/1001" or "30/1")
			frameRateStr := stream.AvgFrameRate
			if frameRateStr == "" || frameRateStr == "0/0" {
				frameRateStr = stream.RFrameRate
			}
			if frameRateStr != "" && frameRateStr != "0/0" {
				parts := strings.Split(frameRateStr, "/")
				if len(parts) == 2 {
					num, _ := strconv.ParseFloat(parts[0], 64)
					den, _ := strconv.ParseFloat(parts[1], 64)
					if den > 0 {
						info.FrameRate = num / den
					}
				}
			}
		} else if stream.CodecType == "audio" {
			info.AudioCodec = stream.CodecName
			streamInfo.Channels = stream.Channels
			if stream.SampleRate != "" {
				if sr, err := strconv.Atoi(stream.SampleRate); err == nil {
					streamInfo.SampleRate = sr
				}
			}
		}

		info.Streams = append(info.Streams, streamInfo)
	}

	return info, nil
}

// GetPresets returns all presets
func (m *Module) GetPresets() []Preset {
	presets := make([]Preset, 0, len(m.presets))
	for _, p := range m.presets {
		presets = append(presets, p)
	}
	return presets
}

// GetPreset returns a specific preset
func (m *Module) GetPreset(id string) (*Preset, error) {
	preset, ok := m.presets[id]
	if !ok {
		return nil, fmt.Errorf("preset not found: %s", id)
	}
	return &preset, nil
}

// ValidateOperations validates a chain of operations
func (m *Module) ValidateOperations(operations []Operation, inputType string) ValidationResult {
	result := ValidationResult{Valid: true}

	for _, op := range operations {
		switch op.Type {
		case "trim", "resize", "compress", "convertFormat", "rotate", "crop",
			"addWatermark", "addSubtitles", "extractAudio", "changeSpeed", "createGif":
			// Valid video operations
			if inputType != "video" && inputType != "" {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Operation '%s' is intended for video", op.Type))
			}
		case "changeBitrate", "adjustVolume", "fadeInOut", "merge", "removeSilence":
			// Valid audio operations
			if inputType != "audio" && inputType != "" {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Operation '%s' is intended for audio", op.Type))
			}
		default:
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("Unknown operation: %s", op.Type))
		}
	}

	return result
}

// GetSupportedFormats returns supported media formats
func (m *Module) GetSupportedFormats() map[string][]FormatInfo {
	return map[string][]FormatInfo{
		"video": {
			{Name: "MP4", Extension: "mp4", MimeTypes: []string{"video/mp4"}, Type: "video", Encodable: true, Decodable: true},
			{Name: "WebM", Extension: "webm", MimeTypes: []string{"video/webm"}, Type: "video", Encodable: true, Decodable: true},
			{Name: "MOV", Extension: "mov", MimeTypes: []string{"video/quicktime"}, Type: "video", Encodable: true, Decodable: true},
			{Name: "AVI", Extension: "avi", MimeTypes: []string{"video/x-msvideo"}, Type: "video", Encodable: true, Decodable: true},
			{Name: "MKV", Extension: "mkv", MimeTypes: []string{"video/x-matroska"}, Type: "video", Encodable: true, Decodable: true},
			{Name: "GIF", Extension: "gif", MimeTypes: []string{"image/gif"}, Type: "video", Encodable: true, Decodable: true},
		},
		"audio": {
			{Name: "MP3", Extension: "mp3", MimeTypes: []string{"audio/mpeg"}, Type: "audio", Encodable: true, Decodable: true},
			{Name: "WAV", Extension: "wav", MimeTypes: []string{"audio/wav"}, Type: "audio", Encodable: true, Decodable: true},
			{Name: "AAC", Extension: "aac", MimeTypes: []string{"audio/aac"}, Type: "audio", Encodable: true, Decodable: true},
			{Name: "FLAC", Extension: "flac", MimeTypes: []string{"audio/flac"}, Type: "audio", Encodable: true, Decodable: true},
			{Name: "OGG", Extension: "ogg", MimeTypes: []string{"audio/ogg"}, Type: "audio", Encodable: true, Decodable: true},
		},
	}
}

// GetAvailableCodecs returns available codecs
func (m *Module) GetAvailableCodecs() map[string][]CodecInfo {
	return map[string][]CodecInfo{
		"video": {
			{Name: "h264", LongName: "H.264 / AVC", Type: "video", Encoding: true, Decoding: true},
			{Name: "h265", LongName: "H.265 / HEVC", Type: "video", Encoding: true, Decoding: true},
			{Name: "vp8", LongName: "VP8", Type: "video", Encoding: true, Decoding: true},
			{Name: "vp9", LongName: "VP9", Type: "video", Encoding: true, Decoding: true},
			{Name: "av1", LongName: "AV1", Type: "video", Encoding: true, Decoding: true},
		},
		"audio": {
			{Name: "aac", LongName: "AAC (Advanced Audio Coding)", Type: "audio", Encoding: true, Decoding: true},
			{Name: "mp3", LongName: "MP3 (MPEG Audio Layer III)", Type: "audio", Encoding: true, Decoding: true},
			{Name: "opus", LongName: "Opus", Type: "audio", Encoding: true, Decoding: true},
			{Name: "flac", LongName: "FLAC (Free Lossless Audio Codec)", Type: "audio", Encoding: true, Decoding: true},
			{Name: "vorbis", LongName: "Vorbis", Type: "audio", Encoding: true, Decoding: true},
		},
	}
}
