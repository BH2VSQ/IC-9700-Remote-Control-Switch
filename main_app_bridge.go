package main

import (
	"context"

	backend "ic9700-remote-io/internal/app"
)

// App is the Wails-facing application wrapper.
// Keeping the bound type in package main makes Wails generate
// frontend/wailsjs/go/main/App.js, which matches the frontend imports.
type App struct {
	backend *backend.App
}

func NewApp() *App {
	return &App{backend: backend.NewApp()}
}

func (a *App) startup(ctx context.Context) {
	a.backend.Startup(ctx)
}

func (a *App) shutdown(ctx context.Context) {
	a.backend.Shutdown(ctx)
}

func (a *App) ListSerialPorts() ([]string, error) {
	return a.backend.ListSerialPorts()
}

func (a *App) Connect(port string, baud int) error {
	return a.backend.Connect(port, baud)
}

func (a *App) Disconnect() error {
	return a.backend.Disconnect()
}

func (a *App) RefreshStatus() backend.Status {
	return a.backend.RefreshStatus()
}

func (a *App) RefreshRadioAssist() backend.RadioAssistStatus {
	return a.backend.RefreshRadioAssist()
}

func (a *App) SetSpeechCompressor(enabled bool) error {
	return a.backend.SetSpeechCompressor(enabled)
}

func (a *App) SetCompLevel(level int) error {
	return a.backend.SetCompLevel(level)
}

func (a *App) SetCWKeyingSpeed(wpm int) error {
	return a.backend.SetCWKeyingSpeed(wpm)
}

func (a *App) SetRFPower(percent int) error {
	return a.backend.SetRFPower(percent)
}

func (a *App) SetDataOffInput(value string) error {
	return a.backend.SetDataOffInput(value)
}

func (a *App) SetDataInput(value string) error {
	return a.backend.SetDataInput(value)
}

func (a *App) SetAllLAN() error {
	return a.backend.SetAllLAN()
}

func (a *App) SetAllUSB() error {
	return a.backend.SetAllUSB()
}

func (a *App) SetUSBOutput(value string) error {
	return a.backend.SetUSBOutput(value)
}

func (a *App) Exit() {
	a.backend.Exit()
}

func (a *App) GetSatelliteStatus() (backend.SatelliteStatus, error) {
	return a.backend.GetSatelliteStatus()
}

func (a *App) SetSatelliteMode(enabled bool) error {
	return a.backend.SetSatelliteMode(enabled)
}

func (a *App) SetSatelliteFrequency(side string, hz uint64) error {
	return a.backend.SetSatelliteFrequency(side, hz)
}

func (a *App) SetSatelliteOperatingMode(side string, mode string) error {
	return a.backend.SetSatelliteOperatingMode(side, mode)
}

func (a *App) OpenSatelliteWindow(theme string) error {
	return a.backend.OpenSatelliteWindow(theme)
}
