package domain

// Health is the derived health of a device.
type Health string

// Health values.
const (
	Healthy  Health = "healthy"
	Warning  Health = "warning"
	Critical Health = "critical"
	Offline  Health = "offline"
)

// DeviceHealth classifies a device.
//
// sample: health() — !online → offline; temp>=80||ram>=90 → critical;
// temp>=72||ram>=80||tmpFree<8 → warning; else healthy.
func DeviceHealth(d Device) Health {
	switch {
	case !d.Online:
		return Offline
	case d.Temp >= 80 || d.RAM >= 90:
		return Critical
	case d.Temp >= 72 || d.RAM >= 80 || d.TmpFreeMB < 8:
		return Warning
	default:
		return Healthy
	}
}

// Drift reports whether a device runs a different config version than its model's desired one.
//
// sample: const drift = d => d.cfgVer !== S.configs[d.model].desired;
func Drift(d Device, desiredCfgVer int) bool {
	return d.CfgVer != desiredCfgVer
}
