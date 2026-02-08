package subscription

// TierLimits defines limits for each subscription tier
type TierLimits struct {
	ConversionMinutes int
	MaxFileSizeBytes  int64
	Priority          string // "default", "high", "critical"
	UseGPUEncoding    bool
}

// Tier limits from plan
var tierLimits = map[string]TierLimits{
	"free": {
		ConversionMinutes: 50,
		MaxFileSizeBytes:  500 * 1024 * 1024, // 500MB
		Priority:          "default",
		UseGPUEncoding:    false,
	},
	"basic": {
		ConversionMinutes: 1500,
		MaxFileSizeBytes:  int64(1.5 * 1024 * 1024 * 1024), // 1.5 GB
		Priority:          "high",
		UseGPUEncoding:    false,
	},
	"standard": {
		ConversionMinutes: 2000,
		MaxFileSizeBytes:  2 * 1024 * 1024 * 1024, // 2 GB
		Priority:          "critical",
		UseGPUEncoding:    false,
	},
	"pro": {
		ConversionMinutes: 4000,
		MaxFileSizeBytes:  5 * 1024 * 1024 * 1024, // 5 GB
		Priority:          "critical",
		UseGPUEncoding:    true,
	},
}

// GetTierLimits returns limits for a tier (defaults to free if unknown)
func GetTierLimits(tier string) TierLimits {
	if limits, ok := tierLimits[tier]; ok {
		return limits
	}
	return tierLimits["free"]
}
