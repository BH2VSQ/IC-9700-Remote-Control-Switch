package civ

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

const (
	RadioAddress      byte = 0xA2
	ControllerAddress byte = 0xE0

	cmdSettings byte = 0x1A
	subSettings byte = 0x05
)

type ModInput byte

const (
	ModMIC    ModInput = 0x00
	ModACC    ModInput = 0x01
	ModMICACC ModInput = 0x02
	ModUSB    ModInput = 0x03
	ModMICUSB ModInput = 0x04
	ModLAN    ModInput = 0x05
)

func (m ModInput) String() string {
	switch m {
	case ModMIC:
		return "MIC"
	case ModACC:
		return "ACC"
	case ModMICACC:
		return "MIC + ACC"
	case ModUSB:
		return "USB"
	case ModMICUSB:
		return "MIC + USB"
	case ModLAN:
		return "LAN"
	default:
		return fmt.Sprintf("Unknown (0x%02X)", byte(m))
	}
}

func ParseModInput(v byte) (ModInput, bool) {
	if v <= 0x05 {
		return ModInput(v), true
	}
	return ModInput(v), false
}

// RadioMode is an IC-9700 operating mode code used by CI-V commands 04/06.
type RadioMode byte

const (
	ModeLSB   RadioMode = 0x00
	ModeUSB   RadioMode = 0x01
	ModeAM    RadioMode = 0x02
	ModeCW    RadioMode = 0x03
	ModeRTTY  RadioMode = 0x04
	ModeFM    RadioMode = 0x05
	ModeCWR   RadioMode = 0x07
	ModeRTTYR RadioMode = 0x08
	ModeDV    RadioMode = 0x17
	ModeDD    RadioMode = 0x22
)

func (m RadioMode) String() string {
	switch m {
	case ModeLSB:
		return "LSB"
	case ModeUSB:
		return "USB"
	case ModeAM:
		return "AM"
	case ModeCW:
		return "CW"
	case ModeRTTY:
		return "RTTY"
	case ModeFM:
		return "FM"
	case ModeCWR:
		return "CW-R"
	case ModeRTTYR:
		return "RTTY-R"
	case ModeDV:
		return "DV"
	case ModeDD:
		return "DD"
	default:
		return fmt.Sprintf("Unknown (0x%02X)", byte(m))
	}
}

func ParseRadioMode(v byte) (RadioMode, bool) {
	mode := RadioMode(v)
	switch mode {
	case ModeLSB, ModeUSB, ModeAM, ModeCW, ModeRTTY, ModeFM, ModeCWR, ModeRTTYR, ModeDV, ModeDD:
		return mode, true
	default:
		return mode, false
	}
}

type USBOutput byte

const (
	USBOutputAF USBOutput = 0x00
	USBOutputIF USBOutput = 0x01
)

func (m USBOutput) String() string {
	switch m {
	case USBOutputAF:
		return "AF"
	case USBOutputIF:
		return "IF"
	default:
		return fmt.Sprintf("Unknown (0x%02X)", byte(m))
	}
}

func ParseUSBOutput(v byte) (USBOutput, bool) {
	if v <= 0x01 {
		return USBOutput(v), true
	}
	return USBOutput(v), false
}

type Frame struct {
	Data []byte
}

func BuildFrame(cmd ...byte) []byte {
	frame := make([]byte, 0, len(cmd)+5)
	frame = append(frame, 0xFE, 0xFE, RadioAddress, ControllerAddress)
	frame = append(frame, cmd...)
	frame = append(frame, 0xFD)
	return frame
}

func FormatFrame(data []byte) string {
	parts := make([]string, len(data))
	for i, v := range data {
		parts[i] = fmt.Sprintf("%02X", v)
	}
	return strings.Join(parts, " ")
}

type Client struct {
	mu          sync.Mutex
	port        serial.Port
	portName    string
	baud        int
	readTimeout time.Duration

	lastTX []byte
	lastRX []byte
}

func NewClient() *Client {
	return &Client{
		baud:        115200,
		readTimeout: 900 * time.Millisecond,
	}
}

func ListPorts() ([]string, error) {
	ports, err := serial.GetPortsList()
	if err != nil {
		return nil, err
	}
	return ports, nil
}

func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.port != nil
}

func (c *Client) PortName() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.portName
}

func (c *Client) Connect(portName string, baud int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.port != nil {
		_ = c.port.Close()
		c.port = nil
	}

	if portName == "" {
		return errors.New("COM port is empty")
	}
	if baud <= 0 {
		return errors.New("invalid baud rate")
	}

	p, err := serial.Open(portName, &serial.Mode{
		BaudRate: baud,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	})
	if err != nil {
		return fmt.Errorf("open %s: %w", portName, err)
	}

	if err := p.SetReadTimeout(50 * time.Millisecond); err != nil {
		_ = p.Close()
		return fmt.Errorf("set COM timeout: %w", err)
	}

	if err := p.ResetInputBuffer(); err != nil {
		_ = p.Close()
		return fmt.Errorf("reset input buffer: %w", err)
	}

	c.port = p
	c.portName = portName
	c.baud = baud
	c.lastTX = nil
	c.lastRX = nil
	return nil
}

func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.port == nil {
		return nil
	}
	err := c.port.Close()
	c.port = nil
	c.portName = ""
	return err
}

func (c *Client) LastFrames() (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return FormatFrame(c.lastTX), FormatFrame(c.lastRX)
}

func (c *Client) sendRead(ctx context.Context, cmd ...byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.port == nil {
		return nil, errors.New("not connected")
	}

	frame := BuildFrame(cmd...)
	c.lastTX = append([]byte(nil), frame...)

	if err := c.port.ResetInputBuffer(); err != nil {
		return nil, fmt.Errorf("reset input buffer: %w", err)
	}

	if _, err := c.port.Write(frame); err != nil {
		return nil, fmt.Errorf("write CI-V: %w", err)
	}

	deadline := time.Now().Add(c.readTimeout)
	var rx bytes.Buffer
	buf := make([]byte, 256)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		n, err := c.port.Read(buf)
		if err != nil {
			return nil, fmt.Errorf("read CI-V: %w", err)
		}
		if n > 0 {
			_, _ = rx.Write(buf[:n])
			frames := ExtractFrames(rx.Bytes())
			for _, f := range frames {
				if isResponseFor(f, cmd...) {
					c.lastRX = append([]byte(nil), f...)
					if isNG(f) {
						return nil, errors.New("radio returned NG")
					}
					return f, nil
				}
			}
		}
	}

	c.lastRX = append([]byte(nil), rx.Bytes()...)
	return nil, fmt.Errorf("CI-V timeout waiting for response to %s", strings.Join(bytesToHex(cmd), " "))
}

func (c *Client) sendSet(ctx context.Context, cmd ...byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.port == nil {
		return errors.New("not connected")
	}

	frame := BuildFrame(cmd...)
	c.lastTX = append([]byte(nil), frame...)

	if err := c.port.ResetInputBuffer(); err != nil {
		return fmt.Errorf("reset input buffer: %w", err)
	}

	if _, err := c.port.Write(frame); err != nil {
		return fmt.Errorf("write CI-V: %w", err)
	}

	deadline := time.Now().Add(c.readTimeout)
	var rx bytes.Buffer
	buf := make([]byte, 256)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := c.port.Read(buf)
		if err != nil {
			return fmt.Errorf("read CI-V: %w", err)
		}
		if n > 0 {
			_, _ = rx.Write(buf[:n])
			frames := ExtractFrames(rx.Bytes())
			for _, f := range frames {
				c.lastRX = append([]byte(nil), f...)
				if isACK(f) {
					return nil
				}
				if isNG(f) {
					return errors.New("radio returned NG")
				}
			}
		}
	}

	c.lastRX = append([]byte(nil), rx.Bytes()...)
	return fmt.Errorf("CI-V timeout waiting for ACK")
}

func (c *Client) GetDataOffModInput(ctx context.Context) (ModInput, error) {
	f, err := c.sendRead(ctx, cmdSettings, subSettings, 0x01, 0x15)
	if err != nil {
		return 0, err
	}
	v, err := parseReadByte(f, cmdSettings, 0x15, "DATA OFF MOD")
	if err != nil {
		return 0, err
	}
	input, ok := ParseModInput(v)
	if !ok {
		return 0, fmt.Errorf("unsupported DATA OFF input value: %02X", byte(v))
	}
	return input, nil
}

func (c *Client) SetDataOffModInput(ctx context.Context, input ModInput) error {
	if input > 0x05 {
		return errors.New("invalid DATA OFF input")
	}
	return c.sendSet(ctx, cmdSettings, subSettings, 0x01, 0x15, byte(input))
}

func (c *Client) GetDataModInput(ctx context.Context) (ModInput, error) {
	f, err := c.sendRead(ctx, cmdSettings, subSettings, 0x01, 0x16)
	if err != nil {
		return 0, err
	}
	v, err := parseReadByte(f, cmdSettings, 0x16, "DATA MOD")
	if err != nil {
		return 0, err
	}
	input, ok := ParseModInput(v)
	if !ok {
		return 0, fmt.Errorf("unsupported DATA input value: %02X", byte(v))
	}
	return input, nil
}

func (c *Client) SetDataModInput(ctx context.Context, input ModInput) error {
	if input > 0x05 {
		return errors.New("invalid DATA input")
	}
	return c.sendSet(ctx, cmdSettings, subSettings, 0x01, 0x16, byte(input))
}

func (c *Client) SetAllLAN(ctx context.Context) error {
	if err := c.SetDataOffModInput(ctx, ModLAN); err != nil {
		return err
	}
	if err := c.SetDataModInput(ctx, ModLAN); err != nil {
		return err
	}
	// LAN audio input is intended for network speech operation, so enable
	// the IC-9700 speech compressor together with the all-LAN shortcut.
	return c.SetSpeechCompressor(ctx, true)
}

// SetAllUSB switches both DATA OFF and DATA ON modulation inputs to USB and
// disables the speech compressor. IC-9700 CI-V uses value 03 for USB on both
// settings.
func (c *Client) SetAllUSB(ctx context.Context) error {
	if err := c.SetDataOffModInput(ctx, ModUSB); err != nil {
		return err
	}
	if err := c.SetDataModInput(ctx, ModUSB); err != nil {
		return err
	}
	return c.SetSpeechCompressor(ctx, false)
}

// GetSpeechCompressor reads SET > Function > Speech compressor.
// Command 16 / sub-command 44 uses 00=OFF and 01=ON.
func (c *Client) GetSpeechCompressor(ctx context.Context) (bool, error) {
	f, err := c.sendRead(ctx, 0x16, 0x44)
	if err != nil {
		return false, err
	}
	v, err := parseCommandDataByte(f, 0x16, 0x44, "Speech compressor")
	if err != nil {
		return false, err
	}
	if v > 0x01 {
		return false, fmt.Errorf("unsupported speech compressor value: %02X", v)
	}
	return v == 0x01, nil
}

// SetSpeechCompressor toggles the IC-9700 speech compressor.
func (c *Client) SetSpeechCompressor(ctx context.Context, enabled bool) error {
	value := byte(0x00)
	if enabled {
		value = 0x01
	}
	return c.sendSet(ctx, 0x16, 0x44, value)
}

// GetCompLevel reads command 14 / sub-command 0E. The radio reports a BCD
// value from 0000 to 0255 for COMP 0 to 10.
func (c *Client) GetCompLevel(ctx context.Context) (int, error) {
	f, err := c.sendRead(ctx, 0x14, 0x0E)
	if err != nil {
		return 0, err
	}
	raw, err := parseBCD255(f, 0x14, 0x0E, "COMP level")
	if err != nil {
		return 0, err
	}
	return clampInt(int(math.Round(float64(raw)*10.0/255.0)), 0, 10), nil
}

// SetCompLevel sets the COMP level 0 to 10 using the CI-V 0000~0255 range.
func (c *Client) SetCompLevel(ctx context.Context, level int) error {
	if level < 0 || level > 10 {
		return fmt.Errorf("COMP level out of range: %d", level)
	}
	raw := int(math.Round(float64(level) * 255.0 / 10.0))
	return c.sendSet(ctx, append([]byte{0x14, 0x0E}, encodeBCD255(raw)...)...)
}

// GetKeyingSpeed reads command 14 / sub-command 0C. The documented range is
// 0000=6 WPM through 0255=48 WPM.
func (c *Client) GetKeyingSpeed(ctx context.Context) (int, error) {
	f, err := c.sendRead(ctx, 0x14, 0x0C)
	if err != nil {
		return 0, err
	}
	raw, err := parseBCD255(f, 0x14, 0x0C, "keying speed")
	if err != nil {
		return 0, err
	}
	return clampInt(int(math.Round(6.0+float64(raw)*42.0/255.0)), 6, 48), nil
}

// SetKeyingSpeed sets CW keying speed in the documented 6~48 WPM range.
func (c *Client) SetKeyingSpeed(ctx context.Context, wpm int) error {
	if wpm < 6 || wpm > 48 {
		return fmt.Errorf("keying speed out of range: %d WPM", wpm)
	}
	raw := int(math.Round(float64(wpm-6) * 255.0 / 42.0))
	return c.sendSet(ctx, append([]byte{0x14, 0x0C}, encodeBCD255(raw)...)...)
}

// GetRFPower reads command 14 / sub-command 0A. The guide specifies the
// complete CI-V range as 0000 minimum to 0255 maximum; the UI normalizes that
// range to 0~100 percent.
func (c *Client) GetRFPower(ctx context.Context) (int, error) {
	f, err := c.sendRead(ctx, 0x14, 0x0A)
	if err != nil {
		return 0, err
	}
	raw, err := parseBCD255(f, 0x14, 0x0A, "RF power")
	if err != nil {
		return 0, err
	}
	return clampInt(int(math.Round(float64(raw)*100.0/255.0)), 0, 100), nil
}

// SetRFPower sets the normalized RF power ratio from 0 to 100 percent.
func (c *Client) SetRFPower(ctx context.Context, percent int) error {
	if percent < 0 || percent > 100 {
		return fmt.Errorf("RF power out of range: %d%%", percent)
	}
	raw := int(math.Round(float64(percent) * 255.0 / 100.0))
	return c.sendSet(ctx, append([]byte{0x14, 0x0A}, encodeBCD255(raw)...)...)
}

func (c *Client) GetUSBOutput(ctx context.Context) (USBOutput, error) {
	f, err := c.sendRead(ctx, cmdSettings, subSettings, 0x01, 0x05)
	if err != nil {
		return 0, err
	}
	v, err := parseReadByte(f, cmdSettings, 0x05, "USB AF/IF Output")
	if err != nil {
		return 0, err
	}
	out, ok := ParseUSBOutput(v)
	if !ok {
		return 0, fmt.Errorf("unsupported USB output value: %02X", byte(v))
	}
	return out, nil
}

func (c *Client) SetUSBOutput(ctx context.Context, output USBOutput) error {
	if output > USBOutputIF {
		return errors.New("invalid USB output mode")
	}
	return c.sendSet(ctx, cmdSettings, subSettings, 0x01, 0x05, byte(output))
}

// GetSatelliteMode reads SET > Function > Satellite Mode.
// IC-9700 uses command 16, sub-command 5A, with 00=OFF and 01=ON.
// This is distinct from the 1A/05 settings selectors.
func (c *Client) GetSatelliteMode(ctx context.Context) (bool, error) {
	f, err := c.sendRead(ctx, 0x16, 0x5A)
	if err != nil {
		return false, err
	}
	v, err := parseCommandDataByte(f, 0x16, 0x5A, "Satellite Mode")
	if err != nil {
		return false, err
	}
	if v > 0x01 {
		return false, fmt.Errorf("unsupported satellite mode value: %02X", v)
	}
	return v == 0x01, nil
}

func (c *Client) SetSatelliteMode(ctx context.Context, enabled bool) error {
	value := byte(0x00)
	if enabled {
		value = 0x01
	}
	// Keep the radio focused on the MAIN/D0 side when changing SAT mode.
	// SkyRoof/SkyCat documents the same main-band focus requirement for
	// toggling satellite/split operation.
	if err := c.SelectSatelliteSide(ctx, 0xD0); err != nil {
		return err
	}
	return c.sendSet(ctx, 0x16, 0x5A, value)
}

// SelectSatelliteSide selects the IC-9700 satellite downlink/main (D0) or
// uplink/sub (D1) side. The reference guide defines D0 as main and D1 as sub.
func (c *Client) SelectSatelliteSide(ctx context.Context, side byte) error {
	if side != 0xD0 && side != 0xD1 {
		return fmt.Errorf("invalid satellite side: %02X", side)
	}
	return c.sendSet(ctx, 0x07, side)
}

func parseFrequency(frame []byte, label string) (uint64, error) {
	if len(frame) < 11 {
		return 0, fmt.Errorf("invalid %s response: %s", label, FormatFrame(frame))
	}
	for i := 0; i < 5; i++ {
		b := frame[len(frame)-6+i]
		if (b>>4) > 9 || (b&0x0F) > 9 {
			return 0, fmt.Errorf("invalid %s BCD frequency: %s", label, FormatFrame(frame))
		}
	}
	digits := [10]uint64{
		uint64(frame[len(frame)-6] & 0x0F),
		uint64(frame[len(frame)-6] >> 4),
		uint64(frame[len(frame)-5] & 0x0F),
		uint64(frame[len(frame)-5] >> 4),
		uint64(frame[len(frame)-4] & 0x0F),
		uint64(frame[len(frame)-4] >> 4),
		uint64(frame[len(frame)-3] & 0x0F),
		uint64(frame[len(frame)-3] >> 4),
		uint64(frame[len(frame)-2] & 0x0F),
		uint64(frame[len(frame)-2] >> 4),
	}
	var hz uint64
	place := uint64(1)
	for i := range digits {
		hz += digits[i] * place
		if i < len(digits)-1 {
			place *= 10
		}
	}
	return hz, nil
}

func encodeFrequency(hz uint64) ([]byte, error) {
	if hz > 1499999999 {
		return nil, fmt.Errorf("frequency out of IC-9700 CI-V range: %d Hz", hz)
	}
	digits := make([]byte, 10)
	for i := 0; i < len(digits); i++ {
		digits[i] = byte(hz % 10)
		hz /= 10
	}
	return []byte{
		digits[1]<<4 | digits[0],
		digits[3]<<4 | digits[2],
		digits[5]<<4 | digits[4],
		digits[7]<<4 | digits[6],
		digits[9]<<4 | digits[8],
	}, nil
}

func (c *Client) GetCurrentFrequency(ctx context.Context) (uint64, error) {
	f, err := c.sendRead(ctx, 0x03)
	if err != nil {
		return 0, err
	}
	return parseFrequency(f, "current frequency")
}

func (c *Client) GetCurrentOperatingMode(ctx context.Context) (RadioMode, error) {
	f, err := c.sendRead(ctx, 0x04)
	if err != nil {
		return 0, err
	}
	if len(f) < 8 {
		return 0, fmt.Errorf("invalid operating mode response: %s", FormatFrame(f))
	}
	mode, ok := ParseRadioMode(f[len(f)-3])
	if !ok {
		return 0, fmt.Errorf("unsupported operating mode: %02X", f[len(f)-3])
	}
	return mode, nil
}

func (c *Client) GetSatelliteFrequency(ctx context.Context, side byte) (uint64, error) {
	if err := c.SelectSatelliteSide(ctx, side); err != nil {
		return 0, err
	}
	f, err := c.sendRead(ctx, 0x03)
	if err != nil {
		return 0, err
	}
	return parseFrequency(f, fmt.Sprintf("Satellite %02X frequency", side))
}

func (c *Client) SetSatelliteFrequency(ctx context.Context, side byte, hz uint64) error {
	if err := c.SelectSatelliteSide(ctx, side); err != nil {
		return err
	}
	data, err := encodeFrequency(hz)
	if err != nil {
		return err
	}
	return c.sendSet(ctx, append([]byte{0x05}, data...)...)
}

func (c *Client) GetSatelliteModeType(ctx context.Context, side byte) (RadioMode, error) {
	if err := c.SelectSatelliteSide(ctx, side); err != nil {
		return 0, err
	}
	f, err := c.sendRead(ctx, 0x04)
	if err != nil {
		return 0, err
	}
	if len(f) != 8 {
		return 0, fmt.Errorf("invalid satellite mode response: %s", FormatFrame(f))
	}
	mode, ok := ParseRadioMode(f[len(f)-3])
	if !ok {
		return 0, fmt.Errorf("unsupported satellite operating mode: %02X", f[len(f)-3])
	}
	return mode, nil
}

func (c *Client) SetSatelliteModeType(ctx context.Context, side byte, mode RadioMode) error {
	if _, ok := ParseRadioMode(byte(mode)); !ok {
		return fmt.Errorf("unsupported satellite operating mode: %02X", byte(mode))
	}
	if err := c.SelectSatelliteSide(ctx, side); err != nil {
		return err
	}
	return c.sendSet(ctx, 0x06, byte(mode))
}

func parseBCD255(frame []byte, cmd, sub byte, label string) (int, error) {
	// IC-9700 response format for two-byte numeric values:
	// FE FE E0 A2 <cmd> <sub> <BCD high> <BCD low> FD
	if len(frame) < 9 {
		return 0, fmt.Errorf("invalid %s response: %s", label, FormatFrame(frame))
	}
	if frame[0] != 0xFE || frame[1] != 0xFE || frame[2] != ControllerAddress || frame[3] != RadioAddress {
		return 0, fmt.Errorf("invalid CI-V address in %s response: %s", label, FormatFrame(frame))
	}
	if frame[4] != cmd || frame[5] != sub {
		return 0, fmt.Errorf("unexpected %s response: %s", label, FormatFrame(frame))
	}
	if frame[len(frame)-1] != 0xFD {
		return 0, fmt.Errorf("invalid %s response terminator: %s", label, FormatFrame(frame))
	}
	hi := frame[len(frame)-3]
	lo := frame[len(frame)-2]
	for _, b := range []byte{hi, lo} {
		if (b>>4) > 9 || (b&0x0F) > 9 {
			return 0, fmt.Errorf("invalid %s BCD value: %s", label, FormatFrame(frame))
		}
	}
	v := int(hi>>4)*1000 + int(hi&0x0F)*100 + int(lo>>4)*10 + int(lo&0x0F)
	if v > 255 {
		return 0, fmt.Errorf("%s value out of range: %d", label, v)
	}
	return v, nil
}

func encodeBCD255(value int) []byte {
	if value < 0 {
		value = 0
	}
	if value > 255 {
		value = 255
	}
	return []byte{
		byte(((value/1000)%10)<<4 | ((value / 100) % 10)),
		byte(((value/10)%10)<<4 | (value % 10)),
	}
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func parseCommandDataByte(frame []byte, cmd, sub byte, label string) (byte, error) {
	// IC-9700 response format for command 16/5A:
	// FE FE E0 A2 16 5A <value> FD
	if len(frame) < 8 {
		return 0, fmt.Errorf("invalid %s response: %s", label, FormatFrame(frame))
	}
	if frame[0] != 0xFE || frame[1] != 0xFE || frame[2] != ControllerAddress || frame[3] != RadioAddress {
		return 0, fmt.Errorf("invalid CI-V address in %s response: %s", label, FormatFrame(frame))
	}
	if frame[4] != cmd || frame[5] != sub {
		return 0, fmt.Errorf("unexpected %s response: %s", label, FormatFrame(frame))
	}
	if frame[len(frame)-1] != 0xFD {
		return 0, fmt.Errorf("invalid %s response terminator: %s", label, FormatFrame(frame))
	}
	return frame[len(frame)-2], nil
}

func parseReadByte(frame []byte, cmd, sub byte, label string) (byte, error) {
	// IC-9700 response format for 1A 05 settings:
	// FE FE E0 A2 1A 05 01 <sub-command> <value> FD
	if len(frame) < 10 {
		return 0, fmt.Errorf("invalid %s response: %s", label, FormatFrame(frame))
	}
	if frame[0] != 0xFE || frame[1] != 0xFE || frame[2] != ControllerAddress || frame[3] != RadioAddress {
		return 0, fmt.Errorf("invalid CI-V address in %s response: %s", label, FormatFrame(frame))
	}

	expected := []byte{cmd, subSettings, 0x01, sub}
	if !bytes.Equal(frame[4:8], expected) {
		return 0, fmt.Errorf("unexpected %s response: %s", label, FormatFrame(frame))
	}
	if frame[len(frame)-1] != 0xFD {
		return 0, fmt.Errorf("invalid %s response terminator: %s", label, FormatFrame(frame))
	}

	// The value is the byte immediately before the FD terminator.
	return frame[len(frame)-2], nil
}

func ExtractFrames(data []byte) [][]byte {
	var frames [][]byte
	start := -1
	for i := 0; i < len(data); i++ {
		if start < 0 {
			if i+1 < len(data) && data[i] == 0xFE && data[i+1] == 0xFE {
				start = i
				i++
			}
			continue
		}
		if data[i] == 0xFD {
			frame := append([]byte(nil), data[start:i+1]...)
			frames = append(frames, frame)
			start = -1
		}
	}
	return frames
}

func isResponseFor(frame []byte, cmd ...byte) bool {
	if len(frame) < 6 || frame[0] != 0xFE || frame[1] != 0xFE || frame[len(frame)-1] != 0xFD {
		return false
	}
	if frame[2] != ControllerAddress || frame[3] != RadioAddress {
		return false
	}
	if len(cmd) == 0 {
		return true
	}
	if len(frame) < 4+len(cmd)+1 {
		return false
	}
	for i, b := range cmd {
		if frame[4+i] != b {
			return false
		}
	}
	return true
}

func isACK(frame []byte) bool {
	return len(frame) == 6 && frame[0] == 0xFE && frame[1] == 0xFE && frame[2] == ControllerAddress && frame[3] == RadioAddress && frame[4] == 0xFB && frame[5] == 0xFD
}

func isNG(frame []byte) bool {
	return len(frame) == 6 && frame[0] == 0xFE && frame[1] == 0xFE && frame[2] == ControllerAddress && frame[3] == RadioAddress && frame[4] == 0xFA && frame[5] == 0xFD
}

func bytesToHex(data []byte) []string {
	out := make([]string, len(data))
	for i, b := range data {
		out[i] = strings.ToUpper(hex.EncodeToString([]byte{b}))
	}
	return out
}
