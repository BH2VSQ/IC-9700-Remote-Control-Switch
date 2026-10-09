package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	backend "ic9700-remote-io/internal/app"
)

// App is the Wails-facing application wrapper.
// Keeping the bound type in package main makes Wails generate
// frontend/wailsjs/go/main/App.js, which matches the frontend imports.
type App struct {
	backend *backend.App
	ctx     context.Context
}

func NewApp() *App {
	return &App{backend: backend.NewApp()}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.backend.Startup(ctx)
}

// SetUIScale updates the native window dimensions as well as the SAT helper scale.
func (a *App) SetUIScale(scale int) error {
	if scale != 125 {
		scale = 100
	}
	if a.ctx != nil {
		runtime.WindowSetSize(a.ctx, 1180*scale/100, 800*scale/100)
	}
	return a.backend.SetUIScale(scale)
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

func (a *App) ConnectSkyCAT(address string) error {
	return a.backend.ConnectSkyCAT(address)
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

func (a *App) OpenSatelliteWindow(theme string, scale int) error {
	return a.backend.OpenSatelliteWindow(theme, scale)
}

func (a *App) CheckForUpdates() (backend.UpdateInfo, error) {
	return a.backend.CheckForUpdates()
}

func (a *App) DownloadAndInstall() error {
	return a.backend.DownloadAndInstall()
}
