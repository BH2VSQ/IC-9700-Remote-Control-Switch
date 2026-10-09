package civ

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// The dedicated SkyCAT port is deliberately NOT the generic rigctl port.
// It only accepts these exact setting selectors. In particular, CI-V 07,
// 03/05, 04/06, PTT, SAT mode writes and arbitrary passthrough are forbidden.
type skycatSetting struct {
	name     string
	selector []byte
	size     int
	writable bool
}

var skycatSettings = []skycatSetting{
	{"DATA_OFF", []byte{0x1A, 0x05, 0x01, 0x15}, 1, true},
	{"DATA_MOD", []byte{0x1A, 0x05, 0x01, 0x16}, 1, true},
	{"USB_OUTPUT", []byte{0x1A, 0x05, 0x01, 0x05}, 1, true},
	{"COMP", []byte{0x16, 0x44}, 1, true},
	{"COMP_LEVEL", []byte{0x14, 0x0E}, 2, true},
	{"KEY_SPEED", []byte{0x14, 0x0C}, 2, true},
	{"RF_POWER", []byte{0x14, 0x0A}, 2, true},
	{"SAT_MODE", []byte{0x16, 0x5A}, 1, false},
}

func skycatMatch(cmd []byte, write bool) (skycatSetting, []byte, error) {
	for _, setting := range skycatSettings {
		n := len(setting.selector)
		if !write && bytes.Equal(cmd, setting.selector) {
			return setting, nil, nil
		}
		if write && setting.writable && len(cmd) == n+setting.size &&
			bytes.Equal(cmd[:n], setting.selector) {
			return setting, cmd[n:], nil
		}
	}
	return skycatSetting{}, nil,
		fmt.Errorf("SkyCAT auxiliary port does not allow CI-V % X (SkyRoof owns SAT, VFO and tuning)", cmd)
}

func (c *Client) IsSkyCAT() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.skycatConn != nil
}

// ConnectSkyCAT creates a single auxiliary TCP client on a loopback-only port.
// It never opens or duplicates RS-BA1's virtual COM device.
func (c *Client) ConnectSkyCAT(addr string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return fmt.Errorf("SkyCAT address must be host:port: %w", err)
	}
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return errors.New("SkyCAT Switch port is localhost-only")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("invalid SkyCAT TCP port")
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 1500*time.Millisecond)
	if err != nil {
		return fmt.Errorf("connect SkyCAT auxiliary port: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.port != nil { _ = c.port.Close(); c.port = nil }
	if c.skycatConn != nil { _ = c.skycatConn.Close() }
	c.skycatConn = conn
	c.skycatReader = bufio.NewReader(conn)
	c.skycatAddr = net.JoinHostPort(host, port)
	c.portName = ""
	c.lastTX, c.lastRX = nil, nil
	// Confirm that this is the restricted Switch API, not the ordinary CAT
	// 4532 listener (which could otherwise appear successfully connected).
	reply, err := c.skycatRequestLocked(context.Background(), "GET SAT_MODE")
	if err != nil || !strings.HasPrefix(reply, "VALUE ") {
		_ = conn.Close()
		c.skycatConn = nil
		c.skycatReader = nil
		c.skycatAddr = ""
		return fmt.Errorf("SkyCAT auxiliary protocol handshake failed: %v", err)
	}
	return nil
}

func (c *Client) skycatRequestLocked(ctx context.Context, request string) (string, error) {
	if c.skycatConn == nil { return "", errors.New("SkyCAT not connected") }
	deadline := time.Now().Add(1500*time.Millisecond)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) { deadline = d }
	if err := c.skycatConn.SetDeadline(deadline); err != nil { return "", err }
	defer c.skycatConn.SetDeadline(time.Time{})
	if _, err := c.skycatConn.Write([]byte(request + "\n")); err != nil {
		return "", fmt.Errorf("SkyCAT write: %w", err)
	}
	response, err := c.skycatReader.ReadString('\n')
	if err != nil { return "", fmt.Errorf("SkyCAT response: %w", err) }
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "ERR ") { return "", fmt.Errorf("SkyCAT: %s", response) }
	return response, nil
}

func (c *Client) skycatReadLocked(ctx context.Context, cmd ...byte) ([]byte, error) {
	setting, _, err := skycatMatch(cmd, false)
	if err != nil { return nil, err }
	c.lastTX = BuildFrame(cmd...)
	response, err := c.skycatRequestLocked(ctx, "GET "+setting.name)
	if err != nil { return nil, err }
	if !strings.HasPrefix(response, "VALUE ") { return nil, fmt.Errorf("unexpected SkyCAT response %q", response) }
	data, err := hex.DecodeString(strings.TrimPrefix(response, "VALUE "))
	if err != nil || len(data) != setting.size {
		return nil, fmt.Errorf("invalid SkyCAT %s readback: %q", setting.name, response)
	}
	// Return exactly the same CI-V reply as the original serial driver so all
	// existing IC-9700 decoders, UI models, and value conversions remain intact.
	frame := append([]byte{0xFE, 0xFE, ControllerAddress, RadioAddress}, cmd...)
	frame = append(frame, data...)
	frame = append(frame, 0xFD)
	c.lastRX = append([]byte(nil), frame...)
	return frame, nil
}

func (c *Client) skycatWriteLocked(ctx context.Context, cmd ...byte) error {
	setting, data, err := skycatMatch(cmd, true)
	if err != nil { return err }
	c.lastTX = BuildFrame(cmd...)
	response, err := c.skycatRequestLocked(ctx, "SET "+setting.name+" "+strings.ToUpper(hex.EncodeToString(data)))
	if err != nil { return err }
	if response != "OK" { return fmt.Errorf("unexpected SkyCAT write response %q", response) }
	c.lastRX = []byte{0xFE, 0xFE, ControllerAddress, RadioAddress, 0xFB, 0xFD}
	return nil
}
