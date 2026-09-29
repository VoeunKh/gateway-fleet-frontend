package fleet

import (
	"time"

	"github.com/voeunkh/gateway-fleet-frontend/internal/domain"
)

// JSON views returned by the read API (see docs/API.md). Times are UTC; zero
// times are omitted as null.

// ModelView is a hardware model.
type ModelView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	SoC     string `json:"soc"`
	Arch    string `json:"arch"`
	RAMMB   int    `json:"ramMB"`
	FlashMB int    `json:"flashMB"`
	OS      string `json:"os"`
}

// BoardCell is one square on the overview fleet board.
type BoardCell struct {
	SN     string        `json:"sn"`
	Health domain.Health `json:"health"`
	FW     string        `json:"fw"`
}

// VersionCount is how many devices of a model run a firmware version.
type VersionCount struct {
	Version string `json:"version"`
	Count   int    `json:"count"`
}

// OverviewModel is one model row of the overview.
type OverviewModel struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Count    int            `json:"count"`
	Board    []BoardCell    `json:"board"`
	Firmware []VersionCount `json:"firmware"`
}

// RolloutMini is an active rollout's progress on the overview.
type RolloutMini struct {
	ID    string              `json:"id"`
	Model string              `json:"model"`
	FW    string              `json:"fw"`
	State domain.RolloutState `json:"state"`
	Done  int                 `json:"done"`
	Total int                 `json:"total"`
}

// Overview is GET /api/overview.
//
// sample: vOverview() — "N gateways, X online. Y need attention, Z have config drift."
type Overview struct {
	Total     int             `json:"total"`
	Online    int             `json:"online"`
	Attention int             `json:"attention"` // critical or offline
	Drifted   int             `json:"drifted"`
	Models    []OverviewModel `json:"models"`
	Rollouts  []RolloutMini   `json:"rollouts"`
	Alerts    []AlertView     `json:"alerts"` // top 5 open or acknowledged
}

// DeviceSummary is one row of the devices list.
type DeviceSummary struct {
	SN       string        `json:"sn"`
	Model    string        `json:"model"`
	HwRev    string        `json:"hwRev"`
	Site     string        `json:"site"`
	FW       string        `json:"fw"`
	CfgVer   int           `json:"cfgVer"`
	Drift    bool          `json:"drift"`
	Health   domain.Health `json:"health"`
	Online   bool          `json:"online"`
	Temp     float64       `json:"temp"`
	LastSeen time.Time     `json:"lastSeen"`
}

// DeviceList is GET /api/devices.
type DeviceList struct {
	Items      []DeviceSummary `json:"items"`
	Matched    int             `json:"matched"` // devices matching the filters
	Total      int             `json:"total"`   // all devices
	NextCursor *string         `json:"nextCursor"`
}

// InterfaceView is a device interface.
type InterfaceView struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Ident string `json:"ident"`
	Up    bool   `json:"up"`
}

// PackageInstall is a package installed on a device.
type PackageInstall struct {
	Name      string `json:"name"`
	Installed string `json:"installed"`
	Latest    string `json:"latest"`
	UpToDate  bool   `json:"upToDate"`
}

// DeviceConfig is the device's desired config, rendered with secrets masked.
type DeviceConfig struct {
	Desired  int    `json:"desired"`
	Reported int    `json:"reported"`
	Drift    bool   `json:"drift"`
	Rendered string `json:"rendered"`
}

// DeviceRollout is the device's place in an active rollout.
type DeviceRollout struct {
	ID    string             `json:"id"`
	FW    string             `json:"fw"`
	State domain.DeviceState `json:"state"`
	Pct   float64            `json:"pct"`
}

// DeviceDetail is GET /api/devices/{sn}.
type DeviceDetail struct {
	DeviceSummary
	ModelInfo  ModelView        `json:"modelInfo"`
	Bricked    bool             `json:"bricked"`
	UptimeH    float64          `json:"uptimeH"`
	CPU        float64          `json:"cpu"`
	RAM        float64          `json:"ram"`
	TmpFreeMB  float64          `json:"tmpFreeMB"`
	RSSI       *float64         `json:"rssi"`
	Interfaces []InterfaceView  `json:"interfaces"`
	Packages   []PackageInstall `json:"packages"`
	Config     DeviceConfig     `json:"config"`
	Rollout    *DeviceRollout   `json:"rollout"`
}

// EventView is a device history entry.
type EventView struct {
	At  time.Time `json:"at"`
	Msg string    `json:"msg"`
}

// EventList is GET /api/devices/{sn}/events.
type EventList struct {
	Items      []EventView `json:"items"`
	NextCursor *string     `json:"nextCursor"`
}

// MetricPoint is one 5-minute rollup.
type MetricPoint struct {
	At      time.Time `json:"at"`
	TempAvg *float64  `json:"tempAvg"`
	TempMax *float64  `json:"tempMax"`
	CPUAvg  *float64  `json:"cpuAvg"`
	RAMAvg  *float64  `json:"ramAvg"`
	TmpMin  *float64  `json:"tmpMin"`
	RSSIAvg *float64  `json:"rssiAvg"`
}

// Metrics is GET /api/devices/{sn}/metrics.
type Metrics struct {
	Range  string        `json:"range"`
	Points []MetricPoint `json:"points"`
}

// FirmwareView is one firmware image.
type FirmwareView struct {
	Model    string  `json:"model"`
	Version  string  `json:"version"`
	Channel  string  `json:"channel"`
	Released string  `json:"released"`
	SizeMB   float64 `json:"sizeMB"`
	File     string  `json:"file"`
	SHA256   string  `json:"sha256"`
	Blocked  bool    `json:"blocked"`
	Devices  int     `json:"devices"`
}

// PackageView is one package with install counts.
type PackageView struct {
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Prev     string   `json:"prev"`
	Channel  string   `json:"channel"`
	Models   []string `json:"models"`
	Desc     string   `json:"desc"`
	UpToDate int      `json:"upToDate"`
	Outdated int      `json:"outdated"`
}

// DiffLineView is one line of a config diff.
type DiffLineView struct {
	Op   string `json:"op"` // "=", "+", "-"
	Text string `json:"text"`
}

// ConfigVersionView is one template version with its diff against the previous one.
type ConfigVersionView struct {
	V    int            `json:"v"`
	By   string         `json:"by"`
	At   time.Time      `json:"at"`
	Note string         `json:"note"`
	Text string         `json:"text"`
	Diff []DiffLineView `json:"diff"` // null for v1
}

// ConfigView is GET /api/config/{model}.
type ConfigView struct {
	Model    string              `json:"model"`
	Desired  int                 `json:"desired"`
	InSync   int                 `json:"inSync"`
	Drifted  int                 `json:"drifted"`
	Versions []ConfigVersionView `json:"versions"`
}

// AlertView is one alert.
type AlertView struct {
	ID         string            `json:"id"`
	SN         string            `json:"sn"`
	Site       string            `json:"site"`
	Kind       domain.AlertKind  `json:"kind"`
	Severity   domain.Severity   `json:"severity"`
	Message    string            `json:"message"`
	State      domain.AlertState `json:"state"`
	At         time.Time         `json:"at"`
	AckedBy    string            `json:"ackedBy,omitempty"`
	ResolvedAt *time.Time        `json:"resolvedAt"`
}

// AlertList is GET /api/alerts.
type AlertList struct {
	Items      []AlertView `json:"items"`
	NextCursor *string     `json:"nextCursor"`
}

// RolloutDeviceView is one device in a rollout.
type RolloutDeviceView struct {
	SN    string             `json:"sn"`
	Wave  int                `json:"wave"`
	State domain.DeviceState `json:"state"`
	Pct   float64            `json:"pct"`
	From  string             `json:"from"`
	Note  string             `json:"note,omitempty"`
}

// RolloutSummary counts devices by outcome.
//
// sample: vRollout() stat row — Updated, In progress, Rolled back, Need on-site recovery,
// Skipped or deferred, Waiting.
type RolloutSummary struct {
	Updated    int `json:"updated"`
	InProgress int `json:"inProgress"`
	RolledBack int `json:"rolledBack"`
	Bricked    int `json:"bricked"`
	Skipped    int `json:"skippedOrDeferred"`
	Waiting    int `json:"waiting"`
}

// RolloutView is one rollout.
type RolloutView struct {
	ID         string              `json:"id"`
	Model      string              `json:"model"`
	FW         string              `json:"fw"`
	Threshold  float64             `json:"threshold"`
	Waves      []int               `json:"waves"` // devices per wave
	Wave       int                 `json:"wave"`
	State      domain.RolloutState `json:"state"`
	Reason     string              `json:"reason"`
	SoakUntil  *time.Time          `json:"soakUntil"`
	CreatedBy  string              `json:"createdBy"`
	CreatedAt  time.Time           `json:"createdAt"`
	FinishedAt *time.Time          `json:"finishedAt"`
	Summary    RolloutSummary      `json:"summary"`
	Devices    []RolloutDeviceView `json:"devices"`
}

// RolloutList is GET /api/rollouts.
type RolloutList struct {
	Items      []RolloutView `json:"items"`
	NextCursor *string       `json:"nextCursor"`
}
