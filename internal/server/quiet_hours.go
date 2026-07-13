package server

import (
	"fmt"
	"time"

	"tronbyt-server/internal/data"
)

func deviceTimeNow(device *data.Device) time.Time {
	loc := time.Local
	if tz := device.GetTimezone(); tz != "" {
		if loaded, err := time.LoadLocation(tz); err == nil {
			loc = loaded
		}
	}
	return time.Now().In(loc)
}

func clearQuietOverride(device *data.Device) {
	device.QuietOverride = nil
	device.QuietOverrideUntil = nil
}

func clearDimModeOverride(device *data.Device) {
	device.DimModeOverride = nil
	device.DimModeOverrideUntil = nil
}

func setQuietOverride(device *data.Device, active bool) (*time.Time, error) {
	if !device.HasEnabledQuietWindow() {
		return nil, fmt.Errorf("quiet hours is not enabled for this device")
	}

	now := deviceTimeNow(device)
	nextChange := device.GetQuietNextChangeAt(now)
	if nextChange == nil {
		return nil, fmt.Errorf("quiet hours schedule is incomplete")
	}

	override := active
	device.QuietOverride = &override
	device.QuietOverrideUntil = nextChange
	return nextChange, nil
}

func setDimModeOverride(device *data.Device, active bool) (*time.Time, error) {
	if !device.DimModeEnabled {
		return nil, fmt.Errorf("dim mode is not enabled for this device")
	}

	now := deviceTimeNow(device)
	nextChange := device.GetDimModeNextChangeAt(now)
	if nextChange == nil {
		return nil, fmt.Errorf("dim mode schedule is incomplete")
	}

	override := active
	device.DimModeOverride = &override
	device.DimModeOverrideUntil = nextChange
	return nextChange, nil
}
