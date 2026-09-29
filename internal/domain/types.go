package domain

import "time"

// Model is a gateway hardware model (GW-100, GW-200, GW-300).
type Model struct {
	ID         string
	Name       string
	SoC        string
	Arch       string
	RAMMB      int
	FlashMB    int
	OS         string
	Interfaces []Interface
}

// Interface is a network or bus interface on a device.
type Interface struct {
	Kind  string // eth, wifi, lte, lora, ble, rs485, usb
	Name  string // lan1, wwan0, ...
	Ident string // MAC, IMEI or ICCID used by search
	Up    bool
}

// Device is one gateway, keyed by serial number.
type Device struct {
	SN        string
	Model     string
	Site      string
	FW        string
	CfgVer    int
	Online    bool
	Bricked   bool
	LastSeen  time.Time
	Temp      float64 // °C
	CPU       float64 // %
	RAM       float64 // %
	TmpFreeMB float64
	RSSI      float64 // dBm, LTE only
	UptimeH   float64
}

// Firmware is a sysupgrade image for one model.
type Firmware struct {
	Model   string
	Version string
	File    string
	SizeMB  float64
	SHA256  string
	Channel string // stable, beta
	Blocked bool
}

// Package is an .ipk available to the fleet.
type Package struct {
	Name    string
	Version string
	Prev    string
	Channel string
	Models  []string
	Desc    string
}

// ConfigVersion is one saved UCI template version for a model.
type ConfigVersion struct {
	Model string
	V     int
	By    string
	At    time.Time
	Note  string
	Text  string
}

// Site holds values available to config templates.
type Site struct {
	Name      string
	NTPServer string
	APN       string
}
