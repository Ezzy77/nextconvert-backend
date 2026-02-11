package subscription

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetTierLimits(t *testing.T) {
	tests := []struct {
		name     string
		tier     string
		expected TierLimits
	}{
		{
			name: "free tier",
			tier: "free",
			expected: TierLimits{
				ConversionMinutes: 50,
				MaxFileSizeBytes:  500 * 1024 * 1024,
				Priority:          "default",
				UseGPUEncoding:    false,
			},
		},
		{
			name: "basic tier",
			tier: "basic",
			expected: TierLimits{
				ConversionMinutes: 1500,
				MaxFileSizeBytes:  int64(1.5 * 1024 * 1024 * 1024),
				Priority:          "high",
				UseGPUEncoding:    false,
			},
		},
		{
			name: "standard tier",
			tier: "standard",
			expected: TierLimits{
				ConversionMinutes: 2000,
				MaxFileSizeBytes:  2 * 1024 * 1024 * 1024,
				Priority:          "critical",
				UseGPUEncoding:    false,
			},
		},
		{
			name: "pro tier",
			tier: "pro",
			expected: TierLimits{
				ConversionMinutes: 4000,
				MaxFileSizeBytes:  5 * 1024 * 1024 * 1024,
				Priority:          "critical",
				UseGPUEncoding:    true,
			},
		},
		{
			name: "unknown tier defaults to free",
			tier: "unknown_tier",
			expected: TierLimits{
				ConversionMinutes: 50,
				MaxFileSizeBytes:  500 * 1024 * 1024,
				Priority:          "default",
				UseGPUEncoding:    false,
			},
		},
		{
			name: "empty tier defaults to free",
			tier: "",
			expected: TierLimits{
				ConversionMinutes: 50,
				MaxFileSizeBytes:  500 * 1024 * 1024,
				Priority:          "default",
				UseGPUEncoding:    false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetTierLimits(tt.tier)
			assert.Equal(t, tt.expected.ConversionMinutes, result.ConversionMinutes)
			assert.Equal(t, tt.expected.MaxFileSizeBytes, result.MaxFileSizeBytes)
			assert.Equal(t, tt.expected.Priority, result.Priority)
			assert.Equal(t, tt.expected.UseGPUEncoding, result.UseGPUEncoding)
		})
	}
}

func TestConversionMinutesFromDuration(t *testing.T) {
	tests := []struct {
		name     string
		seconds  float64
		expected int
	}{
		{
			name:     "zero seconds returns minimum 1 minute",
			seconds:  0,
			expected: 1,
		},
		{
			name:     "negative seconds returns minimum 1 minute",
			seconds:  -10,
			expected: 1,
		},
		{
			name:     "30 seconds rounds up to 1 minute",
			seconds:  30,
			expected: 1,
		},
		{
			name:     "60 seconds equals 1 minute",
			seconds:  60,
			expected: 1,
		},
		{
			name:     "61 seconds rounds up to 2 minutes",
			seconds:  61,
			expected: 2,
		},
		{
			name:     "120 seconds equals 2 minutes",
			seconds:  120,
			expected: 2,
		},
		{
			name:     "300 seconds equals 5 minutes",
			seconds:  300,
			expected: 5,
		},
		{
			name:     "3661 seconds (1 hour 1 minute 1 second) rounds up to 62 minutes",
			seconds:  3661,
			expected: 62,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ConversionMinutesFromDuration(tt.seconds)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTierPriorities(t *testing.T) {
	t.Run("verify tier priorities are correct", func(t *testing.T) {
		assert.Equal(t, "default", GetTierLimits("free").Priority)
		assert.Equal(t, "high", GetTierLimits("basic").Priority)
		assert.Equal(t, "critical", GetTierLimits("standard").Priority)
		assert.Equal(t, "critical", GetTierLimits("pro").Priority)
	})
}

func TestGPUEncoding(t *testing.T) {
	t.Run("only pro tier has GPU encoding enabled", func(t *testing.T) {
		assert.False(t, GetTierLimits("free").UseGPUEncoding)
		assert.False(t, GetTierLimits("basic").UseGPUEncoding)
		assert.False(t, GetTierLimits("standard").UseGPUEncoding)
		assert.True(t, GetTierLimits("pro").UseGPUEncoding)
	})
}

func TestFileSizeLimits(t *testing.T) {
	t.Run("verify file size limits are in ascending order", func(t *testing.T) {
		free := GetTierLimits("free").MaxFileSizeBytes
		basic := GetTierLimits("basic").MaxFileSizeBytes
		standard := GetTierLimits("standard").MaxFileSizeBytes
		pro := GetTierLimits("pro").MaxFileSizeBytes

		assert.True(t, free < basic, "free tier should have smaller file size limit than basic")
		assert.True(t, basic < standard, "basic tier should have smaller file size limit than standard")
		assert.True(t, standard < pro, "standard tier should have smaller file size limit than pro")
	})
}

func TestConversionMinutesLimits(t *testing.T) {
	t.Run("verify conversion minutes are in ascending order", func(t *testing.T) {
		free := GetTierLimits("free").ConversionMinutes
		basic := GetTierLimits("basic").ConversionMinutes
		standard := GetTierLimits("standard").ConversionMinutes
		pro := GetTierLimits("pro").ConversionMinutes

		assert.True(t, free < basic, "free tier should have fewer conversion minutes than basic")
		assert.True(t, basic < standard, "basic tier should have fewer conversion minutes than standard")
		assert.True(t, standard < pro, "standard tier should have fewer conversion minutes than pro")
	})
}
