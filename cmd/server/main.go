package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"patrol-platform/internal/app"
	"patrol-platform/internal/domain"
	"patrol-platform/internal/httpapi"
	"patrol-platform/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	addr := os.Getenv("PATROL_ADDR")
	if addr == "" {
		addr = ":48235"
	}

	stationID := os.Getenv("PATROL_STATION_ID")
	if stationID == "" {
		stationID = "hualong"
	}
	chiefID := os.Getenv("PATROL_CHIEF_ID")
	if chiefID == "" {
		chiefID = "chief-001"
	}

	// Weather defaults to clear conditions; override via env for testing.
	weather := domain.StaticWeatherProvider{
		Snapshot: domain.WeatherSnapshot{
			StationID:        stationID,
			HasPrecipitation: false,
			WindForce:        3,
			ObservedAt:       time.Now(),
		},
	}

	clock := domain.RealClock{}
	st := store.New()

	shiftSvc := app.NewShiftService(st, clock)
	equipSvc := app.NewEquipmentService(st, st, clock, weather)
	alertSvc := app.NewAlertService(st, clock, chiefID)
	patrolSvc := app.NewPatrolService(st, clock)
	archiveSvc := app.NewArchiveService(st, clock)

	srv := httpapi.NewServer(shiftSvc, equipSvc, alertSvc, patrolSvc, archiveSvc, clock, logger)

	// Graceful shutdown on SIGINT/SIGTERM.
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logger.Info("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	if err := srv.Start(addr); err != nil && err != http.ErrServerClosed {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
	logger.Info("server stopped")
}
