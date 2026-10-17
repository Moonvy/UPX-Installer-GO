package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Environment describes the Adobe tooling found on this machine.
type Environment struct {
	Platform           string `json:"platform"`
	InstallerFound     bool   `json:"installerFound"`
	InstallerPath      string `json:"installerPath,omitempty"`
	CreativeCloudFound bool   `json:"creativeCloudFound"`
}

const (
	creativeCloudTimeout = 30 * time.Second
	upiaTimeout          = 5 * time.Minute
)

// upiaCandidates returns where Creative Cloud Desktop keeps Adobe's plugin
// installer agent.
func upiaCandidates() []string {
	relative := filepath.Join("Adobe", "Adobe Desktop Common", "RemoteComponents", "UPI", "UnifiedPluginInstallerAgent")
	switch runtime.GOOS {
	case "darwin":
		root := "/Library/Application Support"
		return []string{
			filepath.Join(root, relative, "UnifiedPluginInstallerAgent.app", "Contents", "MacOS", "UnifiedPluginInstallerAgent"),
			filepath.Join(root, relative, "UnifiedPluginInstallerAgent"),
		}
	case "windows":
		executable := filepath.Join(relative, "UnifiedPluginInstallerAgent.exe")
		paths := []string{}
		for _, variable := range []string{"CommonProgramFiles", "CommonProgramW6432", "CommonProgramFiles(x86)"} {
			if root := os.Getenv(variable); root != "" {
				paths = append(paths, filepath.Join(root, executable))
			}
		}
		return append(paths, filepath.Join(`C:\Program Files\Common Files`, executable))
	}
	return nil
}

// creativeCloudCandidates returns where Creative Cloud Desktop is installed.
func creativeCloudCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Adobe Creative Cloud/ACC/Creative Cloud.app",
			"/Applications/Utilities/Adobe Creative Cloud/ACC/Creative Cloud.app",
		}
	case "windows":
		relative := filepath.Join("Adobe", "Adobe Creative Cloud", "ACC", "Creative Cloud.exe")
		paths := []string{}
		for _, variable := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
			if root := os.Getenv(variable); root != "" {
				paths = append(paths, filepath.Join(root, relative))
			}
		}
		return paths
	}
	return nil
}

func findUPIA() string {
	for _, candidate := range upiaCandidates() {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// processList returns the command line of every running process.
func processList() string {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("ps", "-ax", "-o", "command=")
	case "windows":
		cmd = exec.Command("tasklist", "/NH")
	default:
		return ""
	}
	hideConsole(cmd)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(output)
}

// processRunning reports whether an Adobe app is running. On macOS every
// name has to appear in the process list, which is how an app is told from a
// helper of the same name; on Windows one process image name is enough.
func processRunning(names ...string) bool {
	output := strings.ToLower(processList())
	if output == "" {
		return false
	}
	if runtime.GOOS == "darwin" {
		for _, name := range names {
			if !strings.Contains(output, name) {
				return false
			}
		}
		return true
	}
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		for _, name := range names {
			if strings.HasPrefix(line, name) {
				return true
			}
		}
	}
	return false
}

func creativeCloudRunning() bool {
	switch runtime.GOOS {
	case "darwin":
		return processRunning("/creative cloud.app/contents/macos/creative cloud")
	case "windows":
		return processRunning("creative cloud.exe")
	}
	return false
}

func photoshopRunning() bool {
	switch runtime.GOOS {
	case "darwin":
		return processRunning("/adobe photoshop", ".app/contents/macos/adobe photoshop")
	case "windows":
		return processRunning("photoshop.exe")
	}
	return false
}

func xdRunning() bool {
	switch runtime.GOOS {
	case "darwin":
		return processRunning("/adobe xd", ".app/contents/macos/adobe xd")
	case "windows":
		return processRunning("adobe xd.exe")
	}
	return false
}

func illustratorRunning() bool {
	switch runtime.GOOS {
	case "darwin":
		return processRunning("/adobe illustrator", ".app/contents/macos/adobe illustrator")
	case "windows":
		return processRunning("illustrator.exe")
	}
	return false
}

// ensureCreativeCloudRunning starts Creative Cloud Desktop when it is not
// already running, and reports whether it had to.
func ensureCreativeCloudRunning() (bool, error) {
	if creativeCloudRunning() {
		return false, nil
	}
	application := ""
	for _, candidate := range creativeCloudCandidates() {
		if _, err := os.Stat(candidate); err == nil {
			application = candidate
			break
		}
	}
	if application == "" {
		// Enterprise installations can provide UPIA without the desktop app.
		return false, nil
	}
	if err := launchApplication(application); err != nil {
		return false, fmt.Errorf("无法启动 Creative Cloud Desktop：%w", err)
	}
	deadline := time.Now().Add(creativeCloudTimeout)
	for time.Now().Before(deadline) {
		if creativeCloudRunning() {
			return true, nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false, errors.New("Creative Cloud Desktop 启动超时。请手动启动并登录 Creative Cloud Desktop 后重试。")
}

func launchApplication(application string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", application).Start()
	}
	cmd := exec.Command(application)
	hideConsole(cmd)
	return cmd.Start()
}

// upiaArgs returns the install arguments Adobe's agent expects, which differ
// between the macOS and Windows builds.
func upiaArgs(packagePath string) []string {
	if runtime.GOOS == "windows" {
		return []string{"/install", packagePath}
	}
	return []string{"--install", packagePath}
}

// runUPIA runs Adobe's installer agent on a package and reports what it
// printed, giving up after upiaTimeout.
func runUPIA(executable, packagePath string) (stdout, stderr string, code int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), upiaTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, upiaArgs(packagePath)...)
	hideConsole(cmd)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	runErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", "", -1, errors.New("Adobe 安装服务执行超时（超过 5 分钟）。请确认 Creative Cloud 已登录后重试。")
	}
	stdout, stderr = strings.TrimSpace(out.String()), strings.TrimSpace(errOut.String())
	if runErr != nil {
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) {
			return "", "", -1, fmt.Errorf("无法启动 Adobe 安装服务：%w", runErr)
		}
		return stdout, stderr, exit.ExitCode(), nil
	}
	return stdout, stderr, 0, nil
}

// checkEnvironment reports the Adobe tooling found on this machine.
func checkEnvironment() Environment {
	installer := findUPIA()
	found := false
	for _, candidate := range creativeCloudCandidates() {
		if _, err := os.Stat(candidate); err == nil {
			found = true
			break
		}
	}
	return Environment{
		Platform:           runtime.GOOS,
		InstallerFound:     installer != "",
		InstallerPath:      installer,
		CreativeCloudFound: found,
	}
}
