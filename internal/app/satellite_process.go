package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// SatelliteWindowService owns the native SAT helper process. Wails v2 itself is
// a single-window framework, so the helper is another instance of the same EXE.
// All radio I/O is still performed by the main process through a localhost RPC.
type SatelliteWindowService struct {
	mu      sync.Mutex
	server  *http.Server
	process *exec.Cmd
	token   string
	uiScale int
}

type satelliteRPCRequest struct {
	Side    string `json:"side"`
	Hz      uint64 `json:"hz"`
	Mode    string `json:"mode"`
	Enabled bool   `json:"enabled"`
}

type satelliteRPCResponse struct {
	OK        bool             `json:"ok"`
	Error     string           `json:"error,omitempty"`
	Connected bool             `json:"connected"`
	Satellite *SatelliteStatus `json:"satellite,omitempty"`
	Scale     int              `json:"scale,omitempty"`
}

func NewSatelliteWindowService() *SatelliteWindowService {
	return &SatelliteWindowService{uiScale: 100}
}

func (s *SatelliteWindowService) Start(owner *App, theme string, scale int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.uiScale = normalizeUIScale(scale)
	if s.process != nil {
		if s.process.ProcessState == nil {
			// The helper is already running: clicking SAT should bring it to
			// the foreground instead of creating another window.
			focusSatelliteWindow()
			return nil
		}
		s.process = nil
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("start SAT IPC listener: %w", err)
	}

	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		_ = listener.Close()
		return fmt.Errorf("generate SAT IPC token: %w", err)
	}
	s.token = hex.EncodeToString(tokenBytes)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/sat/state", s.authorized(owner, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeRPC(w, http.StatusMethodNotAllowed, satelliteRPCResponse{OK: false, Error: "method not allowed"})
			return
		}
		status, err := owner.GetSatelliteWindowState()
		if err != nil {
			writeRPC(w, http.StatusServiceUnavailable, satelliteRPCResponse{OK: false, Error: err.Error()})
			return
		}
		writeRPC(w, http.StatusOK, satelliteRPCResponse{OK: true, Connected: status.Connected, Satellite: &status.Satellite})
	}))
	mux.HandleFunc("/api/sat/ui-scale", s.authorized(owner, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeRPC(w, http.StatusMethodNotAllowed, satelliteRPCResponse{OK: false, Error: "method not allowed"})
			return
		}
		s.mu.Lock()
		scale := normalizeUIScale(s.uiScale)
		s.mu.Unlock()
		writeRPC(w, http.StatusOK, satelliteRPCResponse{OK: true, Scale: scale})
	}))
	mux.HandleFunc("/api/sat/mode", s.authorized(owner, func(w http.ResponseWriter, r *http.Request) {
		var req satelliteRPCRequest
		if !decodeRPC(r, &req) {
			writeRPC(w, http.StatusBadRequest, satelliteRPCResponse{OK: false, Error: "invalid request"})
			return
		}
		if err := owner.SetSatelliteMode(req.Enabled); err != nil {
			writeRPC(w, http.StatusBadRequest, satelliteRPCResponse{OK: false, Error: err.Error()})
			return
		}
		writeRPC(w, http.StatusOK, satelliteRPCResponse{OK: true})
	}))
	mux.HandleFunc("/api/sat/frequency", s.authorized(owner, func(w http.ResponseWriter, r *http.Request) {
		var req satelliteRPCRequest
		if !decodeRPC(r, &req) || req.Hz == 0 || strings.TrimSpace(req.Side) == "" {
			writeRPC(w, http.StatusBadRequest, satelliteRPCResponse{OK: false, Error: "invalid frequency request"})
			return
		}
		if err := owner.SetSatelliteFrequency(req.Side, req.Hz); err != nil {
			writeRPC(w, http.StatusBadRequest, satelliteRPCResponse{OK: false, Error: err.Error()})
			return
		}
		writeRPC(w, http.StatusOK, satelliteRPCResponse{OK: true})
	}))
	mux.HandleFunc("/api/sat/mode-type", s.authorized(owner, func(w http.ResponseWriter, r *http.Request) {
		var req satelliteRPCRequest
		if !decodeRPC(r, &req) || strings.TrimSpace(req.Side) == "" || strings.TrimSpace(req.Mode) == "" {
			writeRPC(w, http.StatusBadRequest, satelliteRPCResponse{OK: false, Error: "invalid operating mode request"})
			return
		}
		if err := owner.SetSatelliteOperatingMode(req.Side, req.Mode); err != nil {
			writeRPC(w, http.StatusBadRequest, satelliteRPCResponse{OK: false, Error: err.Error()})
			return
		}
		writeRPC(w, http.StatusOK, satelliteRPCResponse{OK: true})
	}))

	s.server = &http.Server{Handler: mux}
	go func(server *http.Server, l net.Listener) {
		if err := server.Serve(l); err != nil && err != http.ErrServerClosed {
			// The main application owns the lifetime; errors are not user-facing.
		}
	}(s.server, listener)

	exe, err := os.Executable()
	if err != nil {
		_ = s.server.Close()
		_ = listener.Close()
		return fmt.Errorf("locate application executable: %w", err)
	}
	args := []string{
		"--sat",
		"--rpc-port", strconv.Itoa(listener.Addr().(*net.TCPAddr).Port),
		"--rpc-token", s.token,
		"--theme", sanitizeTheme(theme),
		"--ui-scale", strconv.Itoa(normalizeUIScale(scale)),
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = executableDir(exe)
	if err := cmd.Start(); err != nil {
		_ = s.server.Close()
		_ = listener.Close()
		return fmt.Errorf("start SAT window: %w", err)
	}
	s.process = cmd
	go func(started *exec.Cmd) {
		_ = started.Wait()
		s.mu.Lock()
		if s.process == started {
			s.process = nil
		}
		s.mu.Unlock()
	}(cmd)
	return nil
}

func normalizeUIScale(scale int) int {
	if scale == 125 {
		return 125
	}
	return 100
}

// SetUIScale synchronizes the selected scale with the running SAT process.
func (s *SatelliteWindowService) SetUIScale(scale int) {
	s.mu.Lock()
	s.uiScale = normalizeUIScale(scale)
	s.mu.Unlock()
}

func (s *SatelliteWindowService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		_ = s.server.Close()
		s.server = nil
	}
	if s.process != nil && s.process.Process != nil {
		_ = s.process.Process.Kill()
		// The Wait goroutine reaps the child process and clears s.process.
		s.process = nil
	}
}

func (s *SatelliteWindowService) authorized(owner *App, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The SAT webview uses a custom token header, so browser requests are
		// preflighted. CORS headers must be sent before token validation; the
		// preflight itself does not contain the custom header.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-IC9700-SAT-TOKEN")
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Header.Get("X-IC9700-SAT-TOKEN") != s.token {
			writeRPC(w, http.StatusUnauthorized, satelliteRPCResponse{OK: false, Error: "unauthorized"})
			return
		}
		next(w, r)
	}
}

func decodeRPC(r *http.Request, dst any) bool {
	if r.Method != http.MethodPost {
		return false
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dst) == nil
}

func writeRPC(w http.ResponseWriter, status int, payload satelliteRPCResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func sanitizeTheme(theme string) string {
	if strings.EqualFold(theme, "dark") {
		return "dark"
	}
	return "day"
}

func executableDir(exe string) string {
	idx := strings.LastIndexAny(exe, `/\\`)
	if idx < 0 {
		return "."
	}
	return exe[:idx]
}
