package civ

import (
  "bufio"
  "context"
  "fmt"
  "net"
  "strings"
  "sync"
  "testing"
  "time"
)

func TestSkyCATSelectorAllowlist(t *testing.T) {
  for _, tc := range []struct {
    cmd []byte
    write bool
    allowed bool
    name string
  }{
    {[]byte{0x1A, 0x05, 0x01, 0x15}, false, true, "DATA_OFF"},
    {[]byte{0x14, 0x0A, 0x02, 0x55}, true, true, "RF_POWER"},
    {[]byte{0x16, 0x5A}, false, true, "SAT_MODE"},
    {[]byte{0x16, 0x5A, 0x01}, true, false, ""},
    {[]byte{0x07, 0xD1}, true, false, ""},
    {[]byte{0x05, 0, 0, 0, 0, 0}, true, false, ""},
    {[]byte{0x1C, 0, 0}, true, false, ""},
    {[]byte{0x14, 0x02, 0, 0}, true, false, ""},
  } {
    got, _, err := skycatMatch(tc.cmd, tc.write)
    if (err == nil) != tc.allowed {
      t.Fatalf("cmd % X write=%v: allowed=%v err=%v", tc.cmd, tc.write, tc.allowed, err)
    }
    if err == nil && got.name != tc.name { t.Fatalf("got %s want %s", got.name, tc.name) }
  }
}

func TestSkyCATConnectionUsesDedicatedProtocolAndPreservesCivDecoders(t *testing.T) {
  listener, err := net.Listen("tcp", "127.0.0.1:0")
  if err != nil { t.Fatal(err) }
  defer listener.Close()
  var wg sync.WaitGroup
  var mu sync.Mutex
  var requests []string
  wg.Add(1)
  go func() {
    defer wg.Done()
    conn, err := listener.Accept()
    if err != nil { return }
    defer conn.Close()
    in := bufio.NewScanner(conn)
    for in.Scan() {
      request := in.Text()
      mu.Lock()
      requests = append(requests, request)
      mu.Unlock()
      response := "ERR INVALID"
      switch request {
      case "PING": response = "PONG"
      case "GET DATA_OFF": response = "VALUE 05"
      case "SET USB_OUTPUT 01": response = "OK"
      case "GET RF_POWER": response = "VALUE 0128"
      }
      _, _ = fmt.Fprintln(conn, response)
    }
  }()
  addr := listener.Addr().String()
  c := NewClient()
  if err := c.ConnectSkyCAT(addr); err != nil { t.Fatal(err) }
  if !c.IsSkyCAT() || !c.Connected() { t.Fatal("SkyCAT TCP not connected") }
  input, err := c.GetDataOffModInput(context.Background())
  if err != nil || input != ModLAN { t.Fatalf("DATA OFF: %v %v", input, err) }
  if err := c.SetUSBOutput(context.Background(), USBOutputIF); err != nil { t.Fatal(err) }
  power, err := c.GetRFPower(context.Background())
  if err != nil || power < 49 || power > 51 { t.Fatalf("power: %d %v", power, err) }
  if _, err := c.GetSatelliteFrequency(context.Background(), 0xD1); err == nil ||
    !strings.Contains(err.Error(), "does not allow") {
    t.Fatalf("side-changing SAT operation must fail closed: %v", err)
  }
  if err := c.Disconnect(); err != nil { t.Fatal(err) }
  wg.Wait()
  mu.Lock()
  defer mu.Unlock()
  expected := []string{"PING", "GET DATA_OFF", "SET USB_OUTPUT 01", "GET RF_POWER"}
  if strings.Join(requests, "|") != strings.Join(expected, "|") {
    t.Fatalf("requests: %v, expected %v", requests, expected)
  }
}

func TestSkyCATRejectsRemoteEndpoint(t *testing.T) {
  c := NewClient()
  if err := c.ConnectSkyCAT("8.8.8.8:4537"); err == nil {
    t.Fatal("non-loopback endpoint must be rejected")
  }
}

func TestSkyCATTimeoutClosesTransportToAvoidStaleReplies(t *testing.T) {
  client, server := net.Pipe()
  defer server.Close()
  c := NewClient()
  c.skycatConn = client
  c.skycatReader = bufio.NewReader(client)
  c.skycatAddr = "127.0.0.1:4537"

  go func() {
    _, _ = bufio.NewReader(server).ReadString('\n')
    // The radio would return a reply too late for the original request.
    time.Sleep(120 * time.Millisecond)
    _, _ = server.Write([]byte("VALUE 05\n"))
  }()
  ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
  defer cancel()
  if _, err := c.skycatRequestLocked(ctx, "GET DATA_OFF"); err == nil {
    t.Fatal("expected timeout")
  }
  if c.Connected() || c.skycatReader != nil {
    t.Fatal("timed-out socket must not be reused for another CI-V query")
  }
}
