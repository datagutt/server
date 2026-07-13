package data

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func quietDevice(windows ...QuietWindow) Device {
	return Device{QuietHours: QuietHoursConfig{Windows: windows}}
}

// windowAround builds a quiet window (all days) that is guaranteed active at now,
// used to exercise time.Now-based methods deterministically.
func windowAround(now time.Time, mode QuietMode) QuietWindow {
	start := now.Add(-90 * time.Minute)
	end := now.Add(90 * time.Minute)
	return QuietWindow{
		Enabled:   true,
		StartHour: uint8(start.Hour()),
		StartMin:  uint8(start.Minute()),
		EndHour:   uint8(end.Hour()),
		EndMin:    uint8(end.Minute()),
		Days:      0x7F,
		Mode:      mode,
	}
}

func TestGetActiveQuietWindowOvernightWrap(t *testing.T) {
	now := time.Date(2026, time.April, 24, 23, 30, 0, 0, time.UTC) // Friday
	fri := uint8(1) << uint(now.Weekday())
	d := quietDevice(QuietWindow{Enabled: true, StartHour: 22, EndHour: 6, Days: fri, Mode: QuietModeDim})
	assert.NotNil(t, d.GetActiveQuietWindow(now))

	// Just before the window opens, it is inactive.
	before := time.Date(2026, time.April, 24, 21, 30, 0, 0, time.UTC)
	assert.Nil(t, d.GetActiveQuietWindow(before))
}

func TestGetActiveQuietWindowDayMaskExcludes(t *testing.T) {
	now := time.Date(2026, time.April, 24, 23, 30, 0, 0, time.UTC) // Friday
	sat := uint8(1) << uint((int(now.Weekday())+1)%7)
	d := quietDevice(QuietWindow{Enabled: true, StartHour: 22, EndHour: 6, Days: sat, Mode: QuietModeDim})
	// A Saturday-only window's evening does not cover Friday evening.
	assert.Nil(t, d.GetActiveQuietWindow(now))
}

func TestGetActiveQuietWindowWrappedMorningAttribution(t *testing.T) {
	now := time.Date(2026, time.April, 25, 5, 0, 0, 0, time.UTC) // Saturday 05:00
	friday := uint8(1) << uint((int(now.Weekday())+6)%7)
	// A Friday-only overnight window covers into Saturday morning.
	dFri := quietDevice(QuietWindow{Enabled: true, StartHour: 22, EndHour: 6, Days: friday, Mode: QuietModeDim})
	assert.NotNil(t, dFri.GetActiveQuietWindow(now))

	// A Saturday-only window does NOT cover Saturday morning; that portion
	// belongs to the window that started Friday night.
	saturday := uint8(1) << uint(now.Weekday())
	dSat := quietDevice(QuietWindow{Enabled: true, StartHour: 22, EndHour: 6, Days: saturday, Mode: QuietModeDim})
	assert.Nil(t, dSat.GetActiveQuietWindow(now))
}

func TestGetActiveQuietWindowEndEqualsStartNeverActive(t *testing.T) {
	now := time.Date(2026, time.April, 24, 22, 0, 0, 0, time.UTC)
	d := quietDevice(QuietWindow{Enabled: true, StartHour: 22, EndHour: 22, Days: 0x7F, Mode: QuietModeDim})
	assert.Nil(t, d.GetActiveQuietWindow(now))
}

func TestGetActiveQuietWindowDisabledIgnored(t *testing.T) {
	now := time.Date(2026, time.April, 24, 23, 30, 0, 0, time.UTC)
	d := quietDevice(QuietWindow{Enabled: false, StartHour: 22, EndHour: 6, Days: 0x7F, Mode: QuietModeDim})
	assert.Nil(t, d.GetActiveQuietWindow(now))
}

func TestGetEffectiveBrightnessQuietModes(t *testing.T) {
	now := time.Now().In(time.Local)

	off := Device{Brightness: 80, QuietHours: QuietHoursConfig{Windows: []QuietWindow{windowAround(now, QuietModeOff)}}}
	assert.Equal(t, 0, off.GetEffectiveBrightness())

	dim := Device{Brightness: 80, QuietHours: QuietHoursConfig{Windows: []QuietWindow{windowAround(now, QuietModeDim)}}}
	dim.QuietHours.Windows[0].Brightness = 10
	assert.Equal(t, 10, dim.GetEffectiveBrightness())

	app := Device{Brightness: 80, QuietHours: QuietHoursConfig{Windows: []QuietWindow{windowAround(now, QuietModeApp)}}}
	assert.Equal(t, 80, app.GetEffectiveBrightness())

	none := Device{Brightness: 80}
	assert.Equal(t, 80, none.GetEffectiveBrightness())
}

func TestGetQuietIsActiveManualOverride(t *testing.T) {
	now := time.Now()
	overrideUntil := now.Add(30 * time.Minute)

	// Override forces quiet OFF even though a window would otherwise be active,
	// so delivery falls back to the normal brightness.
	forcedOff := false
	d := Device{
		Brightness:         80,
		QuietHours:         QuietHoursConfig{Windows: []QuietWindow{windowAround(now.In(time.Local), QuietModeOff)}},
		QuietOverride:      &forcedOff,
		QuietOverrideUntil: &overrideUntil,
	}
	assert.False(t, d.GetQuietIsActive())
	assert.Equal(t, 80, d.GetEffectiveBrightness())

	// Override forces quiet ON with no scheduled window active.
	forcedOn := true
	d2 := Device{QuietOverride: &forcedOn, QuietOverrideUntil: &overrideUntil}
	assert.True(t, d2.GetQuietIsActive())
}

func TestGetQuietNextChangeAt(t *testing.T) {
	now := time.Date(2026, time.April, 24, 23, 30, 0, 0, time.UTC) // Friday
	d := quietDevice(QuietWindow{Enabled: true, StartHour: 22, EndHour: 6, Days: 0x7F, Mode: QuietModeDim})
	next := d.GetQuietNextChangeAt(now)
	require.NotNil(t, next)
	assert.Equal(t, time.Date(2026, time.April, 25, 6, 0, 0, 0, time.UTC), *next)
}

func TestDeviceGetDimModeIsActiveUsesManualOverride(t *testing.T) {
	override := true
	overrideUntil := time.Now().Add(30 * time.Minute)
	dimTime := "18:00"
	device := Device{
		DimModeEnabled:       true,
		DimTime:              &dimTime,
		DimModeOverride:      &override,
		DimModeOverrideUntil: &overrideUntil,
	}

	assert.True(t, device.GetDimModeIsActive())
}

func TestDeviceHasTimezone(t *testing.T) {
	tz := "America/New_York"

	assert.False(t, (*Device)(nil).HasTimezone())

	assert.False(t, (&Device{}).HasTimezone())

	device := &Device{
		Location: DeviceLocation{Timezone: tz},
	}
	assert.True(t, device.HasTimezone())

	device = &Device{
		Location: DeviceLocation{Description: "Somewhere"},
	}
	assert.False(t, device.HasTimezone())

	device = &Device{
		Timezone: &tz,
	}
	assert.True(t, device.HasTimezone())

	empty := ""
	device = &Device{Timezone: &empty}
	assert.False(t, device.HasTimezone())
}

func TestDeviceSupportsHTTPFirmwareCommands(t *testing.T) {
	httpDevice := Device{
		Type: DeviceTidbytGen1,
		Info: DeviceInfo{ProtocolType: ProtocolHTTP},
	}
	wsDevice := Device{
		Type: DeviceTidbytGen1,
		Info: DeviceInfo{ProtocolType: ProtocolWS},
	}
	otherDevice := Device{
		Type: DeviceOther,
		Info: DeviceInfo{ProtocolType: ProtocolHTTP},
	}

	assert.True(t, httpDevice.SupportsHTTPFirmwareCommands())
	assert.False(t, wsDevice.SupportsHTTPFirmwareCommands())
	assert.False(t, otherDevice.SupportsHTTPFirmwareCommands())
}

func TestDeviceSupportsColorOrder(t *testing.T) {
	cases := []struct {
		name    string
		version string
		want    bool
	}{
		{"no version", "", false},
		{"dev", "dev", true},
		{"below minimum", "v1.6.9", false},
		{"exactly minimum", "v1.7.0", true},
		{"above minimum", "v1.7.3", true},
		{"no v prefix", "1.8.0", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Device{Info: DeviceInfo{FirmwareVersion: tc.version}}
			assert.Equal(t, tc.want, d.SupportsColorOrder())
		})
	}
}
func TestDeviceTypeCanvasAndDisplaySize(t *testing.T) {
	tests := []struct {
		name                        string
		deviceType                  DeviceType
		canvasWidth, canvasHeight   int
		displayWidth, displayHeight int
	}{
		{"classic", DeviceRaspberryPi, 64, 32, 64, 32},
		{"tidbyt", DeviceTidbytGen1, 64, 32, 64, 32},
		{"wide renders 2x", DeviceRaspberryPiWide, 64, 32, 128, 64},
		{"square", DeviceRaspberryPiSquare, 64, 64, 64, 64},
		{"square s3", DeviceMatrixPortalSquare, 64, 64, 64, 64},
		{"unknown falls back", DeviceOther, 64, 32, 64, 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			width, height := tt.deviceType.CanvasSize()
			assert.Equal(t, tt.canvasWidth, width)
			assert.Equal(t, tt.canvasHeight, height)

			width, height = tt.deviceType.DisplaySize()
			assert.Equal(t, tt.displayWidth, width)
			assert.Equal(t, tt.displayHeight, height)
		})
	}
}

// The square MatrixPortal is a firmware device, so it needs its own binaries
// rather than silently inheriting the 64x32 ones: flashing those would light
// the panel at the wrong geometry.
func TestDeviceTypeSquareMatrixPortalHasItsOwnFirmware(t *testing.T) {
	assert.True(t, DeviceMatrixPortalSquare.SupportsFirmware())
	assert.True(t, DeviceMatrixPortalSquare.SupportsOTA())

	firmware := DeviceMatrixPortalSquare.FirmwareFilename(false)
	merged := DeviceMatrixPortalSquare.MergedFilename(false)
	assert.Equal(t, "matrixportal-s3-square.bin", firmware)
	assert.Equal(t, "matrixportal-s3-square_merged.bin", merged)
	assert.NotEqual(t, DeviceMatrixPortal.FirmwareFilename(false), firmware)
	assert.NotEqual(t, DeviceMatrixPortal.MergedFilename(false), merged)
}

func TestDeviceTypeSquareRoundTripsAsSlug(t *testing.T) {
	assert.Equal(t, "raspberrypi_square", DeviceRaspberryPiSquare.Slug())
	assert.Equal(t, DeviceRaspberryPiSquare, StringToDeviceType["raspberrypi_square"])
	assert.Equal(t, "matrixportal_s3_square", DeviceMatrixPortalSquare.Slug())
	assert.Equal(t, DeviceMatrixPortalSquare, StringToDeviceType["matrixportal_s3_square"])

	// Persistence and the API both go through the slug, so an unrecognised
	// value must not silently become a square panel.
	var scanned DeviceType
	require.NoError(t, scanned.Scan("raspberrypi_square"))
	assert.Equal(t, DeviceRaspberryPiSquare, scanned)
	require.NoError(t, scanned.Scan("nonsense"))
	assert.Equal(t, DeviceOther, scanned)
}

// A device type that offers firmware but names no binary would fail only at
// the point someone tries to flash it, so check the pairing directly. Merged
// images are deliberately not required: Pixoticker ships OTA-only.
func TestEveryFirmwareDeviceTypeNamesABinary(t *testing.T) {
	for deviceType, slug := range DeviceTypeToString {
		if !deviceType.SupportsFirmware() {
			continue
		}
		assert.NotEmptyf(t, deviceType.FirmwareFilename(false),
			"%s claims firmware support but names no binary", slug)
	}
}
