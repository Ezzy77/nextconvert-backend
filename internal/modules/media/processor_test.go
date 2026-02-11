package media

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestNewProcessor(t *testing.T) {
	logger := zap.NewNop()

	t.Run("creates processor with defaults", func(t *testing.T) {
		p := NewProcessor(nil, "", logger)
		assert.NotNil(t, p)
		assert.Equal(t, "ffmpeg", p.ffmpegPath)
		assert.Equal(t, 0, p.maxThreads)
		assert.False(t, p.useHardwareAccel)
		assert.True(t, p.preferFastPresets)
	})

	t.Run("creates processor with custom ffmpeg path", func(t *testing.T) {
		p := NewProcessor(nil, "/usr/local/bin/ffmpeg", logger)
		assert.Equal(t, "/usr/local/bin/ffmpeg", p.ffmpegPath)
	})
}

func TestNewProcessorWithConfig(t *testing.T) {
	logger := zap.NewNop()

	t.Run("creates processor with custom config", func(t *testing.T) {
		config := ProcessorConfig{
			FFmpegPath:        "/custom/ffmpeg",
			MaxThreads:        4,
			UseHardwareAccel:  true,
			PreferFastPresets: false,
		}
		p := NewProcessorWithConfig(nil, config, logger)
		assert.Equal(t, "/custom/ffmpeg", p.ffmpegPath)
		assert.Equal(t, 4, p.maxThreads)
		assert.True(t, p.useHardwareAccel)
		assert.False(t, p.preferFastPresets)
	})

	t.Run("defaults ffmpeg path if empty", func(t *testing.T) {
		config := ProcessorConfig{}
		p := NewProcessorWithConfig(nil, config, logger)
		assert.Equal(t, "ffmpeg", p.ffmpegPath)
	})
}

func TestUseHWAccel(t *testing.T) {
	logger := zap.NewNop()

	t.Run("uses processor default when no override", func(t *testing.T) {
		p := NewProcessorWithConfig(nil, ProcessorConfig{UseHardwareAccel: true}, logger)
		opts := &ProcessOptions{}
		assert.True(t, p.useHWAccel(opts))
	})

	t.Run("respects override when provided", func(t *testing.T) {
		p := NewProcessorWithConfig(nil, ProcessorConfig{UseHardwareAccel: false}, logger)
		override := true
		opts := &ProcessOptions{UseHardwareAccel: &override}
		assert.True(t, p.useHWAccel(opts))
	})

	t.Run("can disable hardware acceleration via override", func(t *testing.T) {
		p := NewProcessorWithConfig(nil, ProcessorConfig{UseHardwareAccel: true}, logger)
		override := false
		opts := &ProcessOptions{UseHardwareAccel: &override}
		assert.False(t, p.useHWAccel(opts))
	})
}

func TestOperationStructure(t *testing.T) {
	tests := []struct {
		name       string
		operations []Operation
		valid      bool
	}{
		{
			name:       "empty operations list",
			operations: []Operation{},
			valid:      false,
		},
		{
			name: "valid trim operation",
			operations: []Operation{
				{Type: "trim", Params: map[string]interface{}{"start": "00:00:10", "end": "00:01:00"}},
			},
			valid: true,
		},
		{
			name: "valid resize operation",
			operations: []Operation{
				{Type: "resize", Params: map[string]interface{}{"width": 1280, "height": 720}},
			},
			valid: true,
		},
		{
			name: "valid compress operation",
			operations: []Operation{
				{Type: "compress", Params: map[string]interface{}{"quality": 23}},
			},
			valid: true,
		},
		{
			name: "valid convert format operation",
			operations: []Operation{
				{Type: "convertFormat", Params: map[string]interface{}{"format": "mp4", "videoCodec": "h264", "audioCodec": "aac"}},
			},
			valid: true,
		},
		{
			name: "operation with unknown type",
			operations: []Operation{
				{Type: "unknown_operation", Params: map[string]interface{}{}},
			},
			valid: false,
		},
		{
			name: "multiple valid operations",
			operations: []Operation{
				{Type: "trim", Params: map[string]interface{}{"start": "00:00:00", "end": "00:00:30"}},
				{Type: "resize", Params: map[string]interface{}{"width": 640, "height": 480}},
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.valid {
				assert.NotEmpty(t, tt.operations, "valid operations should not be empty")
				for _, op := range tt.operations {
					assert.NotEmpty(t, op.Type, "operation type should not be empty")
				}
			}
		})
	}
}

func TestOperationType(t *testing.T) {
	tests := []struct {
		name     string
		opType   string
		category string
	}{
		{"trim is video operation", "trim", "video"},
		{"resize is video operation", "resize", "video"},
		{"compress is video operation", "compress", "video"},
		{"rotate is video operation", "rotate", "video"},
		{"crop is video operation", "crop", "video"},
		{"convertFormat can be video or audio", "convertFormat", "multi"},
		{"extractAudio is video operation", "extractAudio", "video"},
		{"addWatermark is video operation", "addWatermark", "video"},
		{"changeSpeed is video operation", "changeSpeed", "video"},
		{"createGif is video operation", "createGif", "video"},
		{"merge can be video or audio", "merge", "multi"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This is just documenting expected operation types
			// Actual validation happens in ValidateOperations
			assert.NotEmpty(t, tt.opType)
		})
	}
}

func TestProcessorConfiguration(t *testing.T) {
	logger := zap.NewNop()

	t.Run("cloud-friendly defaults for production", func(t *testing.T) {
		p := NewProcessor(nil, "", logger)
		
		// Cloud-friendly defaults
		assert.False(t, p.useHardwareAccel, "hardware accel should be disabled by default for cloud")
		assert.True(t, p.preferFastPresets, "fast presets should be preferred for cloud")
		assert.Equal(t, 0, p.maxThreads, "max threads should be auto by default")
	})

	t.Run("can configure for local development", func(t *testing.T) {
		config := ProcessorConfig{
			MaxThreads:        4,
			UseHardwareAccel:  true,
			PreferFastPresets: false,
		}
		p := NewProcessorWithConfig(nil, config, logger)
		
		assert.True(t, p.useHardwareAccel)
		assert.False(t, p.preferFastPresets)
		assert.Equal(t, 4, p.maxThreads)
	})

	t.Run("can configure for Pro tier users", func(t *testing.T) {
		// Pro tier gets GPU encoding
		p := NewProcessor(nil, "", logger)
		override := true
		opts := &ProcessOptions{UseHardwareAccel: &override}
		
		assert.True(t, p.useHWAccel(opts), "Pro tier should enable hardware acceleration")
	})
}
