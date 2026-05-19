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
		ConversionMinutes: 30,
		MaxFileSizeBytes:  500 * 1024 * 1024, // 500 MB
		Priority:          "default",
		UseGPUEncoding:    false,
	},
	"basic": {
		ConversionMinutes: 2000,
		MaxFileSizeBytes:  4 * 1024 * 1024 * 1024, // 4 GB
		Priority:          "high",
		UseGPUEncoding:    false,
	},
	"pro": {
		// Marketed as "unlimited (fair-use 10,000/mo)"
		ConversionMinutes: 10000,
		MaxFileSizeBytes:  10 * 1024 * 1024 * 1024, // 10 GB
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
