package main

import (
	"fmt"
	"strings"
)

// InstallResult is the outcome of an install or a sideload.
type InstallResult struct {
	Success           bool   `json:"success"`
	Message           string `json:"message"`
	Details           string `json:"details,omitempty"`
	CanSideLoad       bool   `json:"canSideLoad"`
	ActivationPending bool   `json:"activationPending"`
}

// installPackage installs a package with Adobe's own installer service, the
// Unified Plugin Installer Agent that ships with Creative Cloud Desktop.
func installPackage(filePath string, pkg Package) (InstallResult, error) {
	if pkg.Kind == KindCEP {
		return InstallResult{
			Message:     "CEP 扩展请使用侧载安装。官方安装服务不用于 .zxp 包。",
			Details:     "侧载会将扩展复制到用户级 CEP 目录，并自动开启调试模式。",
			CanSideLoad: true,
		}, nil
	}
	canSideLoad := len(packageSideloadApps(pkg)) > 0
	installer := findUPIA()
	if installer == "" {
		return InstallResult{
			Message:     "未找到 Adobe Unified Plugin Installer Agent。请先安装或更新 Creative Cloud Desktop。",
			CanSideLoad: canSideLoad,
		}, nil
	}
	started, err := ensureCreativeCloudRunning()
	if err != nil {
		return InstallResult{
			Message:     err.Error(),
			Details:     "请手动启动并登录 Creative Cloud Desktop 后重试。",
			CanSideLoad: canSideLoad,
		}, nil
	}
	hostWasRunning := false
	for _, app := range packageSideloadApps(pkg) {
		if app.isRunning() {
			hostWasRunning = true
			break
		}
	}
	hostSummary := joinAnd(packageHostLabels(pkg))

	stdout, stderr, code, err := runUPIA(installer, filePath)
	if err != nil {
		return InstallResult{
			Message:     err.Error(),
			Details:     "请确认 Creative Cloud Desktop 已安装、已登录，并保持运行。",
			CanSideLoad: canSideLoad,
		}, nil
	}
	parts := []string{}
	for _, part := range []string{stdout, stderr} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if started {
		parts = append(parts, "已自动启动 Creative Cloud Desktop。")
	}
	details := strings.Join(parts, "\n")

	if code == 0 {
		message := fmt.Sprintf("插件已成功安装，打开 %s 即可载入。", hostSummary)
		if hostWasRunning {
			message = fmt.Sprintf("插件已安装；请完全退出并重新打开 %s 以启用插件。", hostSummary)
		}
		return InstallResult{
			Success:           true,
			Message:           message,
			Details:           details,
			ActivationPending: hostWasRunning,
		}, nil
	}
	message := "Adobe 安装服务意外终止。"
	if code > 0 {
		message = fmt.Sprintf("Adobe 安装服务返回错误代码 %d。", code)
	}
	return InstallResult{Message: message, Details: details, CanSideLoad: canSideLoad}, nil
}
