package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"ic9700-remote-io/internal/civ"
)

type App struct {
	mu         sync.Mutex
	opMu       sync.Mutex
	civ        *civ.Client
	ctx        context.Context
	satellite  *SatelliteWindowService
	satStateMu sync.RWMutex
	satState   SatelliteStatus
	satStateOK bool
}

type SatelliteStatus struct {
	Enabled     bool   `json:"enabled"`
	RXFrequency uint64 `json:"rxFrequency"`
	RXMode      string `json:"rxMode"`
	TXFrequency uint64 `json:"txFrequency"`
	TXMode      string `json:"txMode"`
}

type Status struct {
	Connected          bool   `json:"connected"`
	Port               string `json:"port"`
	Transport          string `json:"transport"`
	DataOffInput       string `json:"dataOffInput"`
	DataInput          string `json:"dataInput"`
	USBOutput          string `json:"usbOutput"`
	SatelliteMode      bool   `json:"satelliteMode"`
	SatelliteModeKnown bool   `json:"satelliteModeKnown"`
	LastTX             string `json:"lastTX"`
	LastRX             string `json:"lastRX"`
	Error              string `json:"error"`
}

type RadioAssistStatus struct {
	Connected             bool   `json:"connected"`
	SpeechCompressor      bool   `json:"speechCompressor"`
	SpeechCompressorKnown bool   `json:"speechCompressorKnown"`
	CompLevel             int    `json:"compLevel"`
	CWKeyingSpeed         int    `json:"cwKeyingSpeed"`
	RFPower               int    `json:"rfPower"`
	LastTX                string `json:"lastTX"`
	LastRX                string `json:"lastRX"`
	Error                 string `json:"error"`
}

func NewApp() *App {
	return &App{civ: civ.NewClient(), satellite: NewSatelliteWindowService()}
}

func (a *App) Startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
}

func (a *App) Shutdown(_ context.Context) {
	a.satellite.Stop()
	_ = a.civ.Disconnect()
}

func (a *App) ListSerialPorts() ([]string, error) {
	a.opMu.Lock()
	defer a.opMu.Unlock()

	ports, err := civ.ListPorts()
	if err != nil {
		return nil, err
	}
	sort.Slice(ports, func(i, j int) bool {
		return ports[i] < ports[j]
	})
	return ports, nil
}

func (a *App) Connect(port string, baud int) error {
	a.opMu.Lock()
	defer a.opMu.Unlock()

	if baud == 0 {
		baud = 115200
	}
	if err := a.civ.Connect(port, baud); err != nil {
		return err
	}
	a.invalidateSatelliteCache()
	return nil
}

func (a *App) ConnectSkyCAT(address string) error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	if err := a.civ.ConnectSkyCAT(address); err != nil { return err }
	a.invalidateSatelliteCache()
	return nil
}

func (a *App) Disconnect() error {
	a.opMu.Lock()
	defer a.opMu.Unlock()

	err := a.civ.Disconnect()
	a.invalidateSatelliteCache()
	return err
}

func (a *App) RefreshStatus() Status {
	a.opMu.Lock()
	defer a.opMu.Unlock()

	status := Status{
		Connected: a.civ.Connected(),
		Port:      a.civ.PortName(),
	}
	if a.civ.IsSkyCAT() { status.Transport = "skycat" } else { status.Transport = "serial" }
	if !status.Connected {
		status.Error = "Not connected"
		status.LastTX, status.LastRX = a.civ.LastFrames()
		return status
	}

	ctx := a.civContext()

	if v, err := a.civ.GetDataOffModInput(ctx); err != nil {
		status.Error = err.Error()
	} else {
		status.DataOffInput = v.String()
	}

	if status.Error == "" {
		if v, err := a.civ.GetDataModInput(ctx); err != nil {
			status.Error = err.Error()
		} else {
			status.DataInput = v.String()
		}
	}

	if status.Error == "" {
		if v, err := a.civ.GetUSBOutput(ctx); err != nil {
			status.Error = err.Error()
		} else {
			status.USBOutput = v.String()
		}
	}

	// The SAT window is intentionally disabled on the dedicated SkyCAT port.
	// Do not waste a fourth CI-V read every 5 seconds for a value that the
	// auxiliary UI cannot act on; leave VFO/SAT ownership with SkyRoof.
	if !a.civ.IsSkyCAT() {
		if v, err := a.civ.GetSatelliteMode(ctx); err != nil {
			if status.Error == "" { status.Error = err.Error() }
		} else {
			status.SatelliteMode = v
			status.SatelliteModeKnown = true
		}
	}

	// A timeout closes the auxiliary TCP stream to prevent stale replies
	// contaminating the next CI-V read. Reflect that disconnect immediately.
	status.Connected = a.civ.Connected()
	status.LastTX, status.LastRX = a.civ.LastFrames()
	return status
}

// RefreshRadioAssist reads the radio settings used by the compact auxiliary
// control area. It is intentionally separate from the frequent main status
// refresh so the serial port is not hit with four additional reads every few
// seconds; callers refresh this state after connecting and after control writes.
func (a *App) RefreshRadioAssist() RadioAssistStatus {
	a.opMu.Lock()
	defer a.opMu.Unlock()

	status := RadioAssistStatus{
		Connected:     a.civ.Connected(),
		CompLevel:     -1,
		CWKeyingSpeed: -1,
		RFPower:       -1,
	}
	if !status.Connected {
		status.Error = "Not connected"
		status.LastTX, status.LastRX = a.civ.LastFrames()
		return status
	}

	ctx := a.civContext()
	setError := func(err error) {
		if err != nil && status.Error == "" {
			status.Error = err.Error()
		}
	}

	if v, err := a.civ.GetSpeechCompressor(ctx); err != nil {
		setError(err)
	} else {
		status.SpeechCompressor = v
		status.SpeechCompressorKnown = true
	}

	if v, err := a.civ.GetCompLevel(ctx); err != nil {
		setError(err)
	} else {
		status.CompLevel = v
	}

	if v, err := a.civ.GetKeyingSpeed(ctx); err != nil {
		setError(err)
	} else {
		status.CWKeyingSpeed = v
	}

	if v, err := a.civ.GetRFPower(ctx); err != nil {
		setError(err)
	} else {
		status.RFPower = v
	}

	status.LastTX, status.LastRX = a.civ.LastFrames()
	return status
}

func (a *App) SetDataOffInput(value string) error {
	a.opMu.Lock()
	defer a.opMu.Unlock()

	input, err := parseInput(value)
	if err != nil {
		return err
	}
	return a.civ.SetDataOffModInput(a.civContext(), input)
}

func (a *App) SetDataInput(value string) error {
	a.opMu.Lock()
	defer a.opMu.Unlock()

	input, err := parseInput(value)
	if err != nil {
		return err
	}
	return a.civ.SetDataModInput(a.civContext(), input)
}

func (a *App) SetAllLAN() error {
	a.opMu.Lock()
	defer a.opMu.Unlock()

	return a.civ.SetAllLAN(a.civContext())
}

func (a *App) SetAllUSB() error {
	a.opMu.Lock()
	defer a.opMu.Unlock()

	return a.civ.SetAllUSB(a.civContext())
}

func (a *App) SetSpeechCompressor(enabled bool) error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	return a.civ.SetSpeechCompressor(a.civContext(), enabled)
}

func (a *App) SetCompLevel(level int) error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	return a.civ.SetCompLevel(a.civContext(), level)
}

func (a *App) SetCWKeyingSpeed(wpm int) error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	return a.civ.SetKeyingSpeed(a.civContext(), wpm)
}

func (a *App) SetRFPower(percent int) error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	return a.civ.SetRFPower(a.civContext(), percent)
}

func (a *App) SetUSBOutput(value string) error {
	a.opMu.Lock()
	defer a.opMu.Unlock()

	var output civ.USBOutput
	switch value {
	case "AF":
		output = civ.USBOutputAF
	case "IF":
		output = civ.USBOutputIF
	default:
		return fmt.Errorf("invalid USB output: %s", value)
	}
	return a.civ.SetUSBOutput(a.civContext(), output)
}

func (a *App) Exit() {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx != nil {
		runtime.Quit(ctx)
	}
}

func satSide(value string) (byte, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "RX", "D0", "MAIN":
		return 0xD0, nil
	case "TX", "D1", "SUB":
		return 0xD1, nil
	default:
		return 0, fmt.Errorf("invalid satellite side: %s", value)
	}
}

func parseRadioMode(value string) (civ.RadioMode, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "LSB":
		return civ.ModeLSB, nil
	case "USB":
		return civ.ModeUSB, nil
	case "AM":
		return civ.ModeAM, nil
	case "CW":
		return civ.ModeCW, nil
	case "RTTY":
		return civ.ModeRTTY, nil
	case "FM":
		return civ.ModeFM, nil
	case "CW-R":
		return civ.ModeCWR, nil
	case "RTTY-R":
		return civ.ModeRTTYR, nil
	case "DV":
		return civ.ModeDV, nil
	case "DD":
		return civ.ModeDD, nil
	default:
		return 0, fmt.Errorf("invalid operating mode: %s", value)
	}
}

func (a *App) GetSatelliteStatus() (SatelliteStatus, error) {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	return a.readSatelliteStateLocked(a.civContext())
}

func (a *App) readSatelliteStateLocked(ctx context.Context) (SatelliteStatus, error) {
	var status SatelliteStatus
	if a.civ.IsSkyCAT() {
		return status, fmt.Errorf("SkyCAT auxiliary connection is read-only for satellite state; SkyRoof owns VFO selection")
	}
	if !a.civ.Connected() {
		return status, fmt.Errorf("not connected")
	}

	// Keep the radio focused on the SAT RX/D0 side while reading/toggling SAT.
	if err := a.civ.SelectSatelliteSide(ctx, 0xD0); err != nil {
		return status, err
	}

	enabled, err := a.civ.GetSatelliteMode(ctx)
	if err != nil {
		return status, err
	}
	status.Enabled = enabled
	if !enabled {
		return status, nil
	}

	var mode civ.RadioMode
	status.RXFrequency, err = a.civ.GetCurrentFrequency(ctx)
	if err != nil {
		return status, err
	}
	mode, err = a.civ.GetCurrentOperatingMode(ctx)
	if err != nil {
		return status, err
	}
	status.RXMode = mode.String()

	if err := a.civ.SelectSatelliteSide(ctx, 0xD1); err != nil {
		return status, err
	}
	status.TXFrequency, err = a.civ.GetCurrentFrequency(ctx)
	if err != nil {
		return status, err
	}
	mode, err = a.civ.GetCurrentOperatingMode(ctx)
	if err != nil {
		return status, err
	}
	status.TXMode = mode.String()

	// Return to RX/D0 after every SAT read, matching the normal SAT control flow.
	if err := a.civ.SelectSatelliteSide(ctx, 0xD0); err != nil {
		return status, err
	}

	a.setSatelliteCache(status)
	return status, nil
}

func (a *App) SetSatelliteMode(enabled bool) error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	if a.civ.IsSkyCAT() { return fmt.Errorf("SkyCAT auxiliary port prohibits satellite mode changes; use SkyRoof") }

	ctx := a.civContext()
	if !a.civ.Connected() {
		return fmt.Errorf("not connected")
	}
	if err := a.civ.SelectSatelliteSide(ctx, 0xD0); err != nil {
		return err
	}
	if err := a.civ.SetSatelliteMode(ctx, enabled); err != nil {
		return err
	}
	if !enabled {
		a.setSatelliteCache(SatelliteStatus{Enabled: false})
		return nil
	}
	// Enabling SAT needs one readback to populate the two SAT VFOs. This happens
	// only on the explicit user action, never in the steady-state UI polling loop.
	_, err := a.readSatelliteStateLocked(ctx)
	return err
}

func (a *App) SetSatelliteFrequency(side string, hz uint64) error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	if a.civ.IsSkyCAT() { return fmt.Errorf("SkyCAT auxiliary port prohibits frequency changes; use SkyRoof") }

	ctx := a.civContext()
	if !a.civ.Connected() {
		return fmt.Errorf("not connected")
	}
	if hz == 0 {
		return fmt.Errorf("invalid satellite frequency")
	}
	if err := a.ensureSatelliteEnabledLocked(ctx); err != nil {
		return err
	}
	s, err := satSide(side)
	if err != nil {
		return err
	}
	if err := a.civ.SetSatelliteFrequency(ctx, s, hz); err != nil {
		return err
	}
	if err := a.civ.SelectSatelliteSide(ctx, 0xD0); err != nil {
		return err
	}

	a.updateSatelliteCacheFrequency(strings.ToUpper(strings.TrimSpace(side)), hz)
	return nil
}

func (a *App) SetSatelliteOperatingMode(side string, mode string) error {
	a.opMu.Lock()
	defer a.opMu.Unlock()
	if a.civ.IsSkyCAT() { return fmt.Errorf("SkyCAT auxiliary port prohibits mode changes; use SkyRoof") }

	ctx := a.civContext()
	if !a.civ.Connected() {
		return fmt.Errorf("not connected")
	}
	if err := a.ensureSatelliteEnabledLocked(ctx); err != nil {
		return err
	}
	sideCode, err := satSide(side)
	if err != nil {
		return err
	}
	m, err := parseRadioMode(mode)
	if err != nil {
		return err
	}
	if err := a.civ.SetSatelliteModeType(ctx, sideCode, m); err != nil {
		return err
	}
	if err := a.civ.SelectSatelliteSide(ctx, 0xD0); err != nil {
		return err
	}

	a.updateSatelliteCacheMode(strings.ToUpper(strings.TrimSpace(side)), m.String())
	return nil
}

func (a *App) OpenSatelliteWindow(theme string) error {
	if a.civ.IsSkyCAT() { return fmt.Errorf("Satellite controls remain in SkyRoof when using SkyCAT TCP") }
	return a.satellite.Start(a, theme)
}

type SatelliteWindowState struct {
	Connected bool            `json:"connected"`
	Satellite SatelliteStatus `json:"satellite"`
}

func (a *App) GetSatelliteWindowState() (SatelliteWindowState, error) {
	a.opMu.Lock()
	defer a.opMu.Unlock()

	if !a.civ.Connected() {
		a.invalidateSatelliteCache()
		return SatelliteWindowState{}, nil
	}
	if state, ok := a.getSatelliteCache(); ok {
		return SatelliteWindowState{Connected: true, Satellite: state}, nil
	}

	state, err := a.readSatelliteStateLocked(a.civContext())
	if err != nil {
		return SatelliteWindowState{Connected: true}, err
	}
	return SatelliteWindowState{Connected: true, Satellite: state}, nil
}

func (a *App) invalidateSatelliteCache() {
	a.satStateMu.Lock()
	a.satStateOK = false
	a.satState = SatelliteStatus{}
	a.satStateMu.Unlock()
}

func (a *App) setSatelliteCache(state SatelliteStatus) {
	a.satStateMu.Lock()
	a.satState = state
	a.satStateOK = true
	a.satStateMu.Unlock()
}

func (a *App) getSatelliteCache() (SatelliteStatus, bool) {
	a.satStateMu.RLock()
	state, ok := a.satState, a.satStateOK
	a.satStateMu.RUnlock()
	return state, ok
}

func (a *App) ensureSatelliteEnabledLocked(ctx context.Context) error {
	if state, ok := a.getSatelliteCache(); ok {
		if !state.Enabled {
			return fmt.Errorf("satellite mode is OFF")
		}
		return nil
	}
	state, err := a.readSatelliteStateLocked(ctx)
	if err != nil {
		return err
	}
	if !state.Enabled {
		return fmt.Errorf("satellite mode is OFF")
	}
	return nil
}

func (a *App) updateSatelliteCacheFrequency(side string, hz uint64) {
	state, ok := a.getSatelliteCache()
	if !ok {
		state = SatelliteStatus{Enabled: true}
	}
	if side == "RX" || side == "D0" || side == "MAIN" {
		state.RXFrequency = hz
	} else {
		state.TXFrequency = hz
	}
	a.setSatelliteCache(state)
}

func (a *App) updateSatelliteCacheMode(side string, mode string) {
	state, ok := a.getSatelliteCache()
	if !ok {
		state = SatelliteStatus{Enabled: true}
	}
	if side == "RX" || side == "D0" || side == "MAIN" {
		state.RXMode = mode
	} else {
		state.TXMode = mode
	}
	a.setSatelliteCache(state)
}

func (a *App) civContext() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func parseInput(value string) (civ.ModInput, error) {
	switch value {
	case "MIC":
		return civ.ModMIC, nil
	case "ACC":
		return civ.ModACC, nil
	case "MIC + ACC":
		return civ.ModMICACC, nil
	case "USB":
		return civ.ModUSB, nil
	case "MIC + USB":
		return civ.ModMICUSB, nil
	case "LAN":
		return civ.ModLAN, nil
	default:
		return 0, fmt.Errorf("invalid audio input: %s", value)
	}
}
