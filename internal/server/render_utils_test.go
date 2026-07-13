package server

import (
	"testing"
	"time"

	"tronbyt-server/internal/data"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEffectiveFilters_ModeFiltersOverrideApp(t *testing.T) {
	s := newTestServer(t)
	require.NotNil(t, s)
	warm := data.ColorFilterWarm
	dimmed := data.ColorFilterDimmed
	redshift := data.ColorFilterRedshift
	dimActive := true
	dimUntil := time.Now().Add(time.Hour)

	tests := []struct {
		name     string
		device   data.Device
		app      *data.App
		expected []string
	}{
		{
			name: "dim mode filter overrides app filter",
			device: data.Device{
				DimModeEnabled:       true,
				DimTime:              new("22:00"),
				DimColorFilter:       &dimmed,
				DimModeOverride:      &dimActive,
				DimModeOverrideUntil: &dimUntil,
			},
			app: &data.App{
				ColorFilter: &warm,
			},
			expected: []string{"dimmed"},
		},
		{
			name: "quiet hours suppresses dim mode filter",
			device: data.Device{
				QuietHours:     allDayQuietWindow(data.QuietModeDim),
				DimModeEnabled: true,
				DimTime:        new("00:00"),
				DimColorFilter: &dimmed,
			},
			app: &data.App{
				ColorFilter: &warm,
			},
			expected: []string{"warm"},
		},
		{
			name: "app filter used when mode filter not configured",
			device: data.Device{
				DimModeEnabled: true,
				DimTime:        new("00:00"),
			},
			app: &data.App{
				ColorFilter: &warm,
			},
			expected: []string{"warm"},
		},
		{
			name: "mode filter none falls through to app filter",
			device: data.Device{
				DimModeEnabled:       true,
				DimTime:              new("22:00"),
				DimColorFilter:       new(data.ColorFilterNone),
				DimModeOverride:      &dimActive,
				DimModeOverrideUntil: &dimUntil,
			},
			app: &data.App{
				ColorFilter: &warm,
			},
			expected: []string{"warm"},
		},
		{
			name: "app inherit uses device filter outside mode",
			device: data.Device{
				ColorFilter: &redshift,
			},
			app: &data.App{
				ColorFilter: new(data.ColorFilterInherit),
			},
			expected: []string{"redshift"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filters := s.getEffectiveFilters(&tt.device, tt.app)
			assert.Equal(t, tt.expected, filters)
		})
	}
}

func TestGetEffectiveFilters_ModeOverride(t *testing.T) {
	s := newTestServer(t)
	require.NotNil(t, s)
	warm := data.ColorFilterWarm
	dimmed := data.ColorFilterDimmed

	// A manual quiet override forces quiet hours on, which suppresses the dim
	// mode filter even though dim mode is scheduled for the whole day.
	active := true
	until := time.Now().Add(time.Hour)
	device := data.Device{
		QuietHours:         data.QuietHoursConfig{Windows: []data.QuietWindow{{Enabled: true, StartHour: 22, EndHour: 6, Days: 0x7F, Mode: data.QuietModeDim}}},
		QuietOverride:      &active,
		QuietOverrideUntil: &until,
		DimModeEnabled:     true,
		DimTime:            new("00:00"),
		DimColorFilter:     &dimmed,
	}
	app := &data.App{ColorFilter: &warm}

	filters := s.getEffectiveFilters(&device, app)
	assert.Equal(t, []string{"warm"}, filters)
}

// allDayQuietWindow returns a quiet-hours config with one window covering
// (almost) the whole day on every day of the week.
func allDayQuietWindow(mode data.QuietMode) data.QuietHoursConfig {
	return data.QuietHoursConfig{Windows: []data.QuietWindow{{
		Enabled: true,
		EndHour: 23,
		EndMin:  59,
		Days:    0x7F,
		Mode:    mode,
	}}}
}
