package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	maxArchiveEntries    = 10_000
	maxExtractedTotal    = 1 << 30
	maxExtractedFileSize = 256 << 20
)

// safeComponent replaces every character that a directory name must not hold.
func safeComponent(value string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.' || r == '-' || r == '_':
			return r
		}
		return '_'
	}, value)
}

// safeEntryPath returns an archive entry's path with "/" separators, and
// reports whether it stays inside the archive root.
func safeEntryPath(name string) (string, bool) {
	normalized := strings.ReplaceAll(name, "\\", "/")
	if normalized == "" || strings.HasPrefix(normalized, "/") {
		return "", false
	}
	cleaned := path.Clean(normalized)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}

// within reports whether target stays inside directory.
func within(directory, target string) bool {
	relative, err := filepath.Rel(directory, target)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// archiveRoot returns the directory inside an archive that holds marker.
func archiveRoot(archive *zip.ReadCloser, marker string) (string, error) {
	for _, entry := range archive.File {
		if !strings.HasSuffix(zipEntryName(entry.Name), marker) {
			continue
		}
		relative, ok := safeEntryPath(entry.Name)
		if !ok {
			return "", fmt.Errorf("安装包中没有安全的 %s 路径。", marker)
		}
		root := relative
		for range strings.Split(marker, "/") {
			root = path.Dir(root)
		}
		if root == "." || root == "/" {
			return "", nil
		}
		return root, nil
	}
	return "", fmt.Errorf("安装包中没有安全的 %s 路径。", marker)
}

// extractArchive unpacks the plugin inside a package into destination, taking
// everything below the directory that holds marker.
func extractArchive(filePath, destination, marker, required string) error {
	archive, err := zip.OpenReader(filePath)
	if err != nil {
		return errors.New("安装包无效或已损坏。")
	}
	defer archive.Close()
	if len(archive.File) > maxArchiveEntries {
		return errors.New("安装包文件数量异常，已停止侧载。")
	}
	root, err := archiveRoot(archive, marker)
	if err != nil {
		return err
	}

	total := uint64(0)
	for _, entry := range archive.File {
		total += entry.UncompressedSize64
		if total > maxExtractedTotal {
			return errors.New("安装包展开后超过 1 GB，已停止侧载。")
		}
	}

	if err := os.MkdirAll(destination, 0o755); err != nil {
		return fmt.Errorf("无法创建临时插件目录：%w", err)
	}
	for _, entry := range archive.File {
		relative, ok := safeEntryPath(entry.Name)
		if !ok {
			return fmt.Errorf("安装包包含不安全路径：%s", entry.Name)
		}
		if root != "" {
			if relative == root {
				continue
			}
			if !strings.HasPrefix(relative, root+"/") {
				continue
			}
			relative = strings.TrimPrefix(relative, root+"/")
		}
		if relative == "" {
			continue
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			return errors.New("安装包包含符号链接，已停止侧载。")
		}
		if entry.UncompressedSize64 > maxExtractedFileSize {
			return fmt.Errorf("安装包中的文件过大：%s", entry.Name)
		}
		target := filepath.Join(destination, filepath.FromSlash(relative))
		if !within(destination, target) {
			return fmt.Errorf("安装包包含不安全路径：%s", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("无法创建插件文件夹：%w", err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("无法创建插件文件夹：%w", err)
		}
		if err := writeZipEntry(entry, target); err != nil {
			return err
		}
	}
	if info, err := os.Stat(filepath.Join(destination, filepath.FromSlash(required))); err != nil || info.IsDir() {
		return fmt.Errorf("侧载目录中没有找到 %s。", required)
	}
	return nil
}

func writeZipEntry(entry *zip.File, target string) error {
	source, err := entry.Open()
	if err != nil {
		return fmt.Errorf("无法读取安装包条目：%w", err)
	}
	defer source.Close()
	output, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("无法写入插件文件：%w", err)
	}
	if _, err := io.Copy(output, source); err != nil {
		output.Close()
		return fmt.Errorf("无法解压插件文件：%w", err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("无法写入插件文件：%w", err)
	}
	return nil
}

// extractCCX unpacks a UXP package: the plugin sits beside manifest.json.
func extractCCX(filePath, destination string) error {
	return extractArchive(filePath, destination, "manifest.json", "manifest.json")
}

// extractCEP unpacks a .zxp package: the extension sits beside CSXS/manifest.xml.
func extractCEP(filePath, destination string) error {
	return extractArchive(filePath, destination, "csxs/manifest.xml", "CSXS/manifest.xml")
}

// replaceFile moves temp over destination, which Windows cannot do in place.
func replaceFile(temp, destination string) error {
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(destination); err == nil {
			if err := os.Remove(destination); err != nil {
				return fmt.Errorf("无法替换旧文件：%w", err)
			}
		}
	}
	if err := os.Rename(temp, destination); err != nil {
		return fmt.Errorf("无法启用新文件：%w", err)
	}
	return nil
}

// writeFileAtomically replaces filePath with contents, or creates it, so that
// a failed write never leaves a half written registry file behind.
func writeFileAtomically(filePath string, contents []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(filePath), "."+filepath.Base(filePath)+".openuxp-temp-*")
	if err != nil {
		return fmt.Errorf("无法创建临时文件：%w", err)
	}
	name := temp.Name()
	if _, err := temp.Write(contents); err != nil {
		temp.Close()
		os.Remove(name)
		return fmt.Errorf("无法写入临时文件：%w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		os.Remove(name)
		return fmt.Errorf("无法保存临时文件：%w", err)
	}
	if err := temp.Close(); err != nil {
		os.Remove(name)
		return fmt.Errorf("无法保存临时文件：%w", err)
	}
	if err := replaceFile(name, filePath); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// probeDirectory reports whether the current user can write beside path.
func probeDirectory(directory string) error {
	probe := directory
	for {
		if _, err := os.Stat(probe); err == nil {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return errors.New("找不到可用于权限检测的上级目录。")
		}
		probe = parent
	}
	if info, err := os.Stat(probe); err != nil || !info.IsDir() {
		return errors.New("目标路径的上级位置不是文件夹。")
	}
	file, err := os.CreateTemp(probe, ".openuxp-write-test-*")
	if err != nil {
		return fmt.Errorf("无法写入：%w", err)
	}
	name := file.Name()
	file.Close()
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("无法清理权限检测文件：%w", err)
	}
	return nil
}
