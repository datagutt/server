package serve

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"tronbyt-server/internal/data"
	"tronbyt-server/internal/migration"

	"gorm.io/gorm"
)

func migrateLegacyDB(ctx context.Context, dsn, dataDir string) error {
	// Check for legacy DB for automatic migration
	legacyDBPath := filepath.Join("users", "usersdb.sqlite") // Old Python DB path
	if _, err := os.Stat(legacyDBPath); err == nil && legacyDBPath != dsn {
		slog.Info("Found legacy database, checking if migration is needed", "legacy_db", legacyDBPath, "new_db", dsn)

		tempDB, err := data.Open(dsn, "ERROR")
		if err == nil {
			if tempDB.Migrator().HasTable(&data.User{}) {
				count, err := gorm.G[data.User](tempDB).Count(ctx, "*")
				if err == nil && count > 0 {
					slog.Warn("New database already has users, skipping automatic migration.", "new_db", dsn)
					return nil
				}
			}
			if sqlDB, err := tempDB.DB(); err == nil {
				if err := sqlDB.Close(); err != nil {
					slog.Error("Failed to close temporary DB connection", "error", err)
				}
			}
		}

		// Perform migration
		if err := migration.MigrateLegacyDB(legacyDBPath, dsn, dataDir); err != nil {
			return fmt.Errorf("automatic migration failed: %w", err)
		}
		slog.Info("Automatic migration completed successfully. Renaming legacy DB.", "legacy_db", legacyDBPath)
		if err := os.Rename(legacyDBPath, legacyDBPath+".bak"); err != nil {
			slog.Error("Failed to rename legacy DB after migration", "error", err)
		}
	}

	return nil
}

func sanitizeDB(ctx context.Context, db *gorm.DB) {
	// Sanitize data before migration (fixes v2.0.x empty email constraint issue)
	if db.Migrator().HasTable(&data.User{}) {
		if _, err := gorm.G[data.User](db).Where("email IN ?", []string{"", "none"}).Update(ctx, "email", nil); err != nil {
			slog.Warn("Failed to sanitize empty emails", "error", err)
		}
	}

	// Fix timezone issues
	if db.Migrator().HasTable(&data.Device{}) {
		devices, err := gorm.G[data.Device](db).Where("location LIKE '%\"timezone\":\"None\"'").Find(ctx)
		if err != nil {
			slog.Warn("Failed to get devices with illegal timestamps", "error", err)
		} else {
			for _, device := range devices {
				device.Location.Timezone = ""
				if _, err := gorm.G[data.Device](db).Where("id = ?", device.ID).Update(ctx, "location", device.Location); err != nil {
					slog.Warn("Failed to update device location during sanitization", "device_id", device.ID, "error", err)
				}
			}
		}
	}
}

// convertLegacyNightMode performs a one-time, in-place migration of the old
// night-mode columns into the quiet-hours JSON column for GORM databases that
// were created before the quiet-hours change. It runs after AutoMigrate.
//
// The old night_* columns are orphaned (AutoMigrate never drops columns), so
// this reads them via raw SQL, which is safe across SQLite/MySQL/Postgres, and
// only touches rows whose quiet_hours is still empty. This is legacy-migration
// only: the modern model has no night-mode concept.
func convertLegacyNightMode(ctx context.Context, db *gorm.DB) {
	if !db.Migrator().HasTable(&data.Device{}) {
		return
	}
	// If the legacy column is absent, this DB was created fresh on the new schema.
	if !db.Migrator().HasColumn(&data.Device{}, "night_start") {
		return
	}

	type legacyNightRow struct {
		ID                     string
		NightModeEnabled       bool
		NightModeApp           string
		NightStart             string
		NightEnd               string
		NightBrightness        int
		NightModeOverride      *bool
		NightModeOverrideUntil *time.Time
	}

	var rows []legacyNightRow
	if err := db.WithContext(ctx).Raw(
		"SELECT id, night_mode_enabled, night_mode_app, night_start, night_end, night_brightness, night_mode_override, night_mode_override_until " +
			"FROM devices WHERE quiet_hours IS NULL OR quiet_hours = '' OR quiet_hours = '{}' OR quiet_hours = '{\"windows\":null}'",
	).Scan(&rows).Error; err != nil {
		slog.Warn("Failed to read legacy night-mode columns for conversion", "error", err)
		return
	}

	for _, row := range rows {
		cfg := legacyNightRowToQuietHours(row.NightModeEnabled, row.NightModeApp, row.NightStart, row.NightEnd, row.NightBrightness)
		updates := map[string]any{
			"quiet_hours":          cfg,
			"quiet_override":       row.NightModeOverride,
			"quiet_override_until": row.NightModeOverrideUntil,
		}
		if err := db.WithContext(ctx).Model(&data.Device{ID: row.ID}).Updates(updates).Error; err != nil {
			slog.Warn("Failed to convert legacy night mode to quiet hours", "device_id", row.ID, "error", err)
			continue
		}
		slog.Info("Converted legacy night mode to quiet hours", "device_id", row.ID)
	}
}

func legacyNightRowToQuietHours(enabled bool, app, start, end string, brightness int) data.QuietHoursConfig {
	sh, sm := parseLegacyClockStr(start)
	eh, em := parseLegacyClockStr(end)
	if app == "None" {
		app = ""
	}
	window := data.QuietWindow{
		Enabled:   enabled,
		StartHour: sh,
		StartMin:  sm,
		EndHour:   eh,
		EndMin:    em,
		Days:      0x7F,
		Mode:      data.QuietModeDim,
	}
	if app != "" {
		window.Mode = data.QuietModeApp
		window.AppIname = app
	} else {
		window.Brightness = data.Brightness(brightness)
	}
	return data.QuietHoursConfig{Windows: []data.QuietWindow{window}}
}

func parseLegacyClockStr(value string) (uint8, uint8) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0
	}
	return uint8(hour), uint8(minute)
}

func singleUserWarning(ctx context.Context, db *gorm.DB, singleUserAutoLogin bool) {
	// Single User Auto-Login Warning
	userCount, err := gorm.G[data.User](db).Count(ctx, "*")
	if err != nil {
		slog.Error("Failed to count users for auto-login warning", "error", err)
	} else if singleUserAutoLogin && userCount == 1 {
		slog.Warn(`
======================================================================
⚠️  SINGLE-USER AUTO-LOGIN MODE IS ENABLED
======================================================================
Authentication is DISABLED for private network connections!

This mode automatically logs in the single user without password.

SECURITY REQUIREMENTS:
  ✓ Only works when exactly 1 user exists
  ✓ Only works from trusted networks:
    - Localhost (127.0.0.1, ::1)
    - Private IPv4 networks (192.168.x.x, 10.x.x.x, 172.16.x.x)
    - IPv6 local ranges (Unique Local Addresses fc00::/7, commonly fd00::/8)
    - IPv6 link-local (fe80::/10)
  ✓ Public IP connections still require authentication

To disable: Set SINGLE_USER_AUTO_LOGIN=0 in your .env file
======================================================================`)
	}
}
