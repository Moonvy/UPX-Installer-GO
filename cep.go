package main

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// cepDebugVersions lists the CEP versions that must accept unsigned
// extensions before a sideloaded .zxp can load.
var cepDebugVersions = []int{6, 7, 8, 9, 10, 11, 12, 13, 14}

func cepDebugKey(version int) string {
	return fmt.Sprintf("CSXS.%d", version)
}

// cepDebugEnabled reports whether PlayerDebugMode is already set.
func cepDebugEnabled(version int) bool {
	key := cepDebugKey(version)
	switch runtime.GOOS {
	case "darwin":
		output, err := exec.Command("defaults", "read", "com.adobe."+key, "PlayerDebugMode").Output()
		return err == nil && strings.TrimSpace(string(output)) == "1"
	case "windows":
		cmd := exec.Command("reg", "query", `HKCU\Software\Adobe\`+key, "/v", "PlayerDebugMode")
		hideConsole(cmd)
		output, err := cmd.Output()
		if err != nil {
			return false
		}
		for _, line := range strings.Split(string(output), "\n") {
			fields := strings.Fields(strings.ToLower(line))
			if len(fields) > 1 && strings.Contains(fields[0], "playerdebugmode") && fields[len(fields)-1] == "1" {
				return true
			}
		}
	}
	return false
}

// enableCEPDebugVersion turns on PlayerDebugMode for one CEP version, in the
// current user's preferences or registry.
func enableCEPDebugVersion(version int) error {
	key := cepDebugKey(version)
	switch runtime.GOOS {
	case "darwin":
		if err := exec.Command("defaults", "write", "com.adobe."+key, "PlayerDebugMode", "-string", "1").Run(); err != nil {
			return fmt.Errorf("无法开启 %s 调试模式：%w", key, err)
		}
		return nil
	case "windows":
		cmd := exec.Command("reg", "add", `HKCU\Software\Adobe\`+key,
			"/v", "PlayerDebugMode", "/t", "REG_SZ", "/d", "1", "/f")
		hideConsole(cmd)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("无法开启 %s 调试模式：%w", key, err)
		}
		return nil
	}
	return errors.New("当前系统不支持开启 CEP 调试模式。")
}

// cepDebugStatus reports whether every CEP version already allows debugging,
// with the keys it checked.
func cepDebugStatus() (bool, []string) {
	keys := make([]string, 0, len(cepDebugVersions))
	enabled := true
	for _, version := range cepDebugVersions {
		keys = append(keys, cepDebugKey(version))
		if enabled && !cepDebugEnabled(version) {
			enabled = false
		}
	}
	return enabled, keys
}

// enableCEPDebugMode turns on PlayerDebugMode for every CEP version.
func enableCEPDebugMode() ([]string, error) {
	enabled := []string{}
	failures := []string{}
	for _, version := range cepDebugVersions {
		if err := enableCEPDebugVersion(version); err != nil {
			failures = append(failures, err.Error())
			continue
		}
		enabled = append(enabled, cepDebugKey(version))
	}
	if len(enabled) == 0 {
		if len(failures) == 0 {
			return nil, errors.New("无法开启 CEP 调试模式。")
		}
		return nil, errors.New(strings.Join(failures, "\n"))
	}
	return enabled, nil
}
