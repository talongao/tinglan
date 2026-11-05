package plugin

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// maxPluginFiles caps the number of entries accepted from one archive. A
// plugin is a small program plus a few assets, so a huge archive is a mistake
// or a decompression bomb.
const maxPluginFiles = 4096

// maxPluginBytes caps the uncompressed size of a plugin archive.
const maxPluginBytes = 256 << 20

// ExtractZip unpacks a plugin archive into dest. Entries must stay inside
// dest: absolute paths, parent traversal and symlinks are rejected instead of
// silently skipped, because a broken archive should fail the whole install.
func ExtractZip(src, dest string) error {
	reader, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("不是有效的 zip：%w", err)
	}
	defer reader.Close()
	if len(reader.File) == 0 {
		return errors.New("zip 是空的")
	}
	if len(reader.File) > maxPluginFiles {
		return fmt.Errorf("zip 内文件过多（%d）", len(reader.File))
	}
	var total int64
	for _, entry := range reader.File {
		clean := path.Clean(strings.ReplaceAll(entry.Name, "\\", "/"))
		if clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.Contains(clean, "/../") {
			return fmt.Errorf("zip 中含有不安全的路径：%s", entry.Name)
		}
		mode := entry.Mode()
		if !mode.IsDir() && !mode.IsRegular() {
			return fmt.Errorf("zip 中含有不支持的文件类型：%s", entry.Name)
		}
		total += int64(entry.UncompressedSize64)
		if total > maxPluginBytes {
			return fmt.Errorf("解压后超过 %d MiB 上限", maxPluginBytes>>20)
		}
		target := filepath.Join(dest, filepath.FromSlash(clean))
		if mode.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extractFile(entry, target, mode.Perm()); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(entry *zip.File, target string, perm os.FileMode) error {
	source, err := entry.Open()
	if err != nil {
		return err
	}
	defer source.Close()
	if perm&0o111 != 0 {
		perm = 0o755
	} else {
		perm = 0o644
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(source, maxPluginBytes)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// PluginRoot finds the directory holding source.json. Archives commonly wrap
// everything in a single top-level folder, so the search is one level deep
// before giving up.
func PluginRoot(dir string) (string, error) {
	if _, err := os.Stat(filepath.Join(dir, ManifestFile)); err == nil {
		return dir, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var found []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		candidate := filepath.Join(dir, entry.Name())
		if _, err := os.Stat(filepath.Join(candidate, ManifestFile)); err == nil {
			found = append(found, candidate)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return "", fmt.Errorf("找不到 %s：这个目录不是音源插件", ManifestFile)
	default:
		return "", fmt.Errorf("目录中有多个插件（%d 个 %s），请一次只添加一个", len(found), ManifestFile)
	}
}
