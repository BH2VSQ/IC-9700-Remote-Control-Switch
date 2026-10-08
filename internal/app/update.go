package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const updateRepo = "BH2VSQ/IC-9700-Remote-Control-Switch"

// UpdateInfo describes the result of a GitHub release check.
type UpdateInfo struct {
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	HasUpdate      bool   `json:"hasUpdate"`
	ReleaseName    string `json:"releaseName"`
	ReleaseNotes   string `json:"releaseNotes"`
	ReleaseURL     string `json:"releaseUrl"`
	AssetURL       string `json:"assetUrl"`
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// CheckForUpdates queries the latest GitHub release and reports whether it is
// newer than the running build.
func (a *App) CheckForUpdates() (UpdateInfo, error) {
	rel, err := fetchLatestRelease()
	if err != nil {
		return UpdateInfo{}, err
	}
	info := UpdateInfo{
		CurrentVersion: Version,
		LatestVersion:  stripVersionPrefix(rel.TagName),
		ReleaseName:    rel.Name,
		ReleaseNotes:   rel.Body,
		ReleaseURL:     rel.HTMLURL,
	}
	info.AssetURL = findExeAsset(rel)
	info.HasUpdate = compareVersions(Version, info.LatestVersion) < 0
	return info, nil
}

// DownloadAndInstall downloads the latest release binary, stages it next to the
// running executable, spawns a detached helper that replaces the executable
// after this process exits, and quits the application.
func (a *App) DownloadAndInstall() error {
	rel, err := fetchLatestRelease()
	if err != nil {
		return err
	}
	latest := stripVersionPrefix(rel.TagName)
	if compareVersions(Version, latest) >= 0 {
		return fmt.Errorf("当前已是最新版本 v%s", Version)
	}
	assetURL := findExeAsset(rel)
	if assetURL == "" {
		return fmt.Errorf("最新版本中没有找到可执行文件")
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位程序路径失败: %w", err)
	}
	newExe := exe + ".new"

	if err := downloadFile(assetURL, newExe); err != nil {
		return err
	}

	script, err := writeUpdaterScript(exe, newExe, os.Getpid())
	if err != nil {
		return err
	}
	if err := launchUpdater(script); err != nil {
		return fmt.Errorf("启动更新程序失败: %w", err)
	}

	// Let the detached updater replace the binary, then relaunch it.
	a.Exit()
	return nil
}

func fetchLatestRelease() (*githubRelease, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+updateRepo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "IC9700-Remote-IO-Updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("查询 GitHub 更新失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("仓库还没有发布任何版本")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API 返回状态 %d", resp.StatusCode)
	}
	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("解析 GitHub 响应失败: %w", err)
	}
	return &rel, nil
}

func findExeAsset(rel *githubRelease) string {
	for _, asset := range rel.Assets {
		if strings.HasSuffix(strings.ToLower(asset.Name), ".exe") {
			return asset.BrowserDownloadURL
		}
	}
	return ""
}

func downloadFile(url, dest string) error {
	client := &http.Client{Timeout: 10 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "IC9700-Remote-IO-Updater")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载更新失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败，HTTP 状态 %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("无法写入更新文件: %w", err)
	}
	n, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("保存更新文件失败: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("保存更新文件失败: %w", closeErr)
	}
	if n == 0 {
		_ = os.Remove(dest)
		return fmt.Errorf("下载到的更新文件为空")
	}
	return nil
}

func writeUpdaterScript(exe, newExe string, pid int) (string, error) {
	script := filepath.Join(os.TempDir(), fmt.Sprintf("ic9700-update-%d.cmd", pid))
	content := "@echo off\r\n" +
		"setlocal\r\n" +
		":waitloop\r\n" +
		"ping -n 2 127.0.0.1 >nul\r\n" +
		"tasklist /FI \"PID eq " + strconv.Itoa(pid) + "\" 2>nul | find /I \"" + strconv.Itoa(pid) + "\" >nul\r\n" +
		"if not errorlevel 1 goto waitloop\r\n" +
		"ping -n 3 127.0.0.1 >nul\r\n" +
		":retry\r\n" +
		"move /y \"" + newExe + "\" \"" + exe + "\" >nul 2>&1\r\n" +
		"if errorlevel 1 (\r\n" +
		"  ping -n 2 127.0.0.1 >nul\r\n" +
		"  goto retry\r\n" +
		")\r\n" +
		"start \"\" \"" + exe + "\"\r\n" +
		"endlocal\r\n"
	if err := os.WriteFile(script, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("写入更新脚本失败: %w", err)
	}
	return script, nil
}

func launchUpdater(script string) error {
	cmd := exec.Command("cmd", "/c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	return cmd.Start()
}

func stripVersionPrefix(s string) string {
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "v"), "V")
}

func parseVersion(s string) []int {
	s = stripVersionPrefix(s)
	if s == "" || strings.EqualFold(s, "dev") {
		return []int{0, 0, 0}
	}
	parts := strings.Split(s, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		n, digits := 0, 0
		for _, c := range p {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
			digits++
		}
		if digits == 0 && strings.TrimSpace(p) != "" {
			// Non-numeric segment: fall back to 0 to keep comparisons stable.
			n = 0
		}
		nums = append(nums, n)
	}
	return nums
}

func compareVersions(a, b string) int {
	av, bv := parseVersion(a), parseVersion(b)
	n := len(av)
	if len(bv) > n {
		n = len(bv)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(av) {
			x = av[i]
		}
		if i < len(bv) {
			y = bv[i]
		}
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}
