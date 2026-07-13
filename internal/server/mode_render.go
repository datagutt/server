package server

import (
	"context"
	"log/slog"
	"time"

	"tronbyt-server/internal/data"

	"gorm.io/gorm"
)

type deviceModeSnapshot struct {
	QuietActive bool
	DimActive   bool
	DimFilter   string
}

func snapshotDeviceMode(device *data.Device) deviceModeSnapshot {
	snap := deviceModeSnapshot{
		QuietActive: device.GetQuietIsActive(),
		DimActive:   device.GetDimModeIsActive(),
	}
	if device.DimColorFilter != nil {
		snap.DimFilter = string(*device.DimColorFilter)
	}
	return snap
}

func (s *Server) invalidateDeviceAppRenders(ctx context.Context, device *data.Device) {
	if _, err := gorm.G[data.App](s.DB).Where("device_id = ?", device.ID).Update(ctx, "last_render", time.Time{}); err != nil {
		slog.Error("Failed to invalidate app renders", "device", device.ID, "error", err)
		return
	}
	for _, app := range device.Apps {
		if app != nil {
			app.LastRender = time.Time{}
		}
	}
}

func (s *Server) invalidateDeviceAppRendersIfModeChanged(ctx context.Context, device *data.Device, before deviceModeSnapshot) {
	if snapshotDeviceMode(device) == before {
		return
	}
	s.invalidateDeviceAppRenders(ctx, device)
}
