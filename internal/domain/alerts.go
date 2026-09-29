package domain

import (
	"errors"
	"fmt"
	"time"
)

// AlertKind identifies what an alert is about; alerts dedupe on (SN, Kind).
type AlertKind string

// Alert kinds.
const (
	AlertOffline AlertKind = "offline"
	AlertTemp    AlertKind = "temp"
	AlertRAM     AlertKind = "ram"
	AlertBricked AlertKind = "bricked"
)

// Severity of an alert.
type Severity string

// Severities.
const (
	SevCritical Severity = "critical"
	SevWarning  Severity = "warning"
)

// AlertState is the lifecycle state: open → ack → resolved.
type AlertState string

// Alert states.
const (
	AlertOpen     AlertState = "open"
	AlertAcked    AlertState = "ack"
	AlertResolved AlertState = "resolved"
)

// Alert thresholds and retention.
const (
	OfflineAlertAfter = 10 * time.Minute
	AlertRetention    = 90 * 24 * time.Hour
)

// Alert is a raised condition on a device.
type Alert struct {
	ID         string
	SN         string
	Kind       AlertKind
	Severity   Severity
	Message    string
	State      AlertState
	At         time.Time
	AckedBy    string
	ResolvedAt time.Time
}

// AlertCondition is an alert that should be open.
type AlertCondition struct {
	Kind     AlertKind
	Severity Severity
	Message  string
}

// AlertDelta is what EvaluateAlerts wants changed.
type AlertDelta struct {
	Open    []AlertCondition // new alerts to create
	Resolve []Alert          // existing unresolved alerts to resolve
}

// BrickedAlert is raised when a device stops responding after an update.
//
// sample: msg:`No response after update to ${r.fw}. Needs on-site recovery.`
func BrickedAlert(fw string) AlertCondition {
	return AlertCondition{AlertBricked, SevCritical, fmt.Sprintf("No response after update to %s. Needs on-site recovery.", fw)}
}

// EvaluateAlerts compares a device's current state with its unresolved alerts.
// unresolved must hold only this device's alerts that are open or acked.
// A bricked alert is raised by the rollout engine (BrickedAlert) and resolved here
// once the device is no longer bricked.
//
// sample: checkAlerts() — raise() dedupes by (sn, kind) while not resolved.
func EvaluateAlerts(d Device, now time.Time, unresolved []Alert) AlertDelta {
	active := map[AlertKind]AlertCondition{}
	resolvable := map[AlertKind]bool{}

	// sample: if (!d.online && Date.now()-d.lastSeen > 10*60e3 && !d.bricked) raise(...) else if (d.online) resolve(d,'offline');
	switch {
	case !d.Online && now.Sub(d.LastSeen) > OfflineAlertAfter && !d.Bricked:
		active[AlertOffline] = AlertCondition{AlertOffline, SevCritical, "Offline for more than 10 minutes"}
	case d.Online:
		resolvable[AlertOffline] = true
	}
	// sample: if (d.online && d.temp >= 80) raise(d,'temp','critical',`Temperature ${d.temp} °C (limit 80 °C)`); else resolve(d,'temp');
	if d.Online && d.Temp >= 80 {
		active[AlertTemp] = AlertCondition{AlertTemp, SevCritical, fmt.Sprintf("Temperature %s °C (limit 80 °C)", jsNum(d.Temp))}
	} else {
		resolvable[AlertTemp] = true
	}
	// sample: if (d.online && d.ram >= 90) raise(d,'ram','warning',`RAM use ${d.ram}% (limit 90%)`); else resolve(d,'ram');
	if d.Online && d.RAM >= 90 {
		active[AlertRAM] = AlertCondition{AlertRAM, SevWarning, fmt.Sprintf("RAM use %s%% (limit 90%%)", jsNum(d.RAM))}
	} else {
		resolvable[AlertRAM] = true
	}
	if !d.Bricked {
		resolvable[AlertBricked] = true
	}

	var delta AlertDelta
	have := map[AlertKind]bool{}
	for _, a := range unresolved {
		if a.State == AlertResolved {
			continue
		}
		have[a.Kind] = true
		if resolvable[a.Kind] {
			delta.Resolve = append(delta.Resolve, a)
		}
	}
	for _, k := range []AlertKind{AlertOffline, AlertTemp, AlertRAM} {
		if c, ok := active[k]; ok && !have[k] {
			delta.Open = append(delta.Open, c)
		}
	}
	return delta
}

// ErrAlertNotOpen is returned when acknowledging an alert that is not open.
var ErrAlertNotOpen = errors.New("alert is not open")

// AckAlert moves an open alert to ack.
//
// sample: a.state='ack'; a.by=ROLE_NAME[S.role];
func AckAlert(a *Alert, by string) error {
	if a.State != AlertOpen {
		return ErrAlertNotOpen
	}
	a.State = AlertAcked
	a.AckedBy = by
	return nil
}

// ResolveAlert marks an alert resolved.
func ResolveAlert(a *Alert, now time.Time) {
	if a.State != AlertResolved {
		a.State = AlertResolved
		a.ResolvedAt = now
	}
}

// AlertExpired reports whether a resolved alert is past retention.
func AlertExpired(a Alert, now time.Time) bool {
	return a.State == AlertResolved && now.Sub(a.ResolvedAt) > AlertRetention
}
