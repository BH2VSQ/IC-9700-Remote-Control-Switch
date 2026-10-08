package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed frontend/dist
var assets embed.FS

func main() {
	if hasArg("--sat") {
		if err := runSatelliteWindow(); err != nil {
			panic(err)
		}
		return
	}
	if err := runMainWindow(); err != nil {
		panic(err)
	}
}

func runMainWindow() error {
	application := NewApp()
	return wails.Run(&options.App{
		Title:            "IC-9700 Remote Audio Control",
		Width:            980,
		Height:           700,
		MinWidth:         820,
		MinHeight:        620,
		Frameless:        false,
		DisableResize:    true,
		BackgroundColour: &options.RGBA{R: 12, G: 17, B: 24, A: 255},
		OnStartup:        application.startup,
		OnShutdown:       application.shutdown,
		Assets:           assets,
		Bind: []interface{}{
			application,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
	})
}

type SatelliteShell struct {
	ctx context.Context
}

func (s *SatelliteShell) startup(ctx context.Context) {
	s.ctx = ctx
}

func (s *SatelliteShell) Close() {
	if s.ctx != nil {
		runtime.Quit(s.ctx)
	}
}

func runSatelliteWindow() error {
	port, token, theme, err := satelliteArgs()
	if err != nil {
		return err
	}

	shell := &SatelliteShell{}
	handler, err := satelliteAssetHandler(port, token, theme)
	if err != nil {
		return err
	}

	return wails.Run(&options.App{
		Title:         "IC-9700 SAT",
		Width:         520,
		Height:        600,
		DisableResize: true,
		StartHidden:   true,
		AssetsHandler: handler,
		OnStartup:     shell.startup,
		OnDomReady: func(ctx context.Context) {
			runtime.WindowSetTitle(ctx, "IC-9700 SAT")
			runtime.WindowCenter(ctx)
			// The helper is created hidden. Only after the SAT page is ready do we
			// briefly promote it to the foreground so the first open is visible
			// without exposing an unpainted/blank window. The user's persistent
			// Always-On-Top choice is controlled from the SAT page afterwards.
			runtime.WindowSetAlwaysOnTop(ctx, true)
			runtime.WindowShow(ctx)
			runtime.WindowSetAlwaysOnTop(ctx, false)
		},
		Bind: []interface{}{
			shell,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
	})
}

func satelliteAssetHandler(port int, token, theme string) (http.Handler, error) {
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return nil, fmt.Errorf("open embedded SAT assets: %w", err)
	}
	fileServer := http.FileServer(http.FS(dist))
	configScript := fmt.Sprintf(
		"<script>window.__SAT_CONFIG__={port:%d,token:%s,theme:%s};</script>",
		port, strconv.Quote(token), strconv.Quote(theme),
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" || path == "index.html" || path == "sat.html" {
			data, readErr := fs.ReadFile(dist, "sat.html")
			if readErr != nil {
				http.Error(w, readErr.Error(), http.StatusInternalServerError)
				return
			}
			html := strings.Replace(string(data), "</head>", configScript+"</head>", 1)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
			return
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}

func hasArg(name string) bool {
	for _, arg := range os.Args[1:] {
		if arg == name {
			return true
		}
	}
	return false
}

func satelliteArgs() (int, string, string, error) {
	fs := flag.NewFlagSet("sat", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	_ = fs.Bool("sat", false, "run the SAT helper window")
	port := fs.Int("rpc-port", 0, "main process SAT RPC port")
	token := fs.String("rpc-token", "", "main process SAT RPC token")
	theme := fs.String("theme", "day", "UI theme")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return 0, "", "", err
	}
	if *port <= 0 || *port > 65535 {
		return 0, "", "", fmt.Errorf("invalid SAT RPC port: %d", *port)
	}
	if *token == "" {
		return 0, "", "", fmt.Errorf("missing SAT RPC token")
	}
	if *theme != "dark" {
		*theme = "day"
	}
	return *port, *token, *theme, nil
}

// Keep strconv linked in builds where future platform-specific argument
// handling is added without changing the command-line contract.
var _ = strconv.IntSize
