package web

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestFrontendEmbedMatchesDiskDist 确保 embed 包含 dist 产物中的全部文件。
// Go embed 默认会忽略以 "_" 或 "." 开头的文件（例如 Vite 生成的
// _plugin-vue_export-helper-*.js 公共 chunk），会导致浏览器加载这些
// chunk 时收到 SPA fallback 的 index.html（text/html）而拒绝执行。
// 该测试用于守护使用 //go:embed all:dist 而不是 //go:embed dist/*。
func TestFrontendEmbedMatchesDiskDist(t *testing.T) {
	distFS, err := fs.Sub(frontendFS, "dist")
	if err != nil {
		t.Fatalf("fs.Sub(frontendFS, %q): %v", "dist", err)
	}

	// 收集磁盘上 dist 的全部文件（相对路径）
	var diskFiles []string
	err = filepath.Walk("dist", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel("dist", path)
		if err != nil {
			return err
		}
		diskFiles = append(diskFiles, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk disk dist: %v", err)
	}
	if len(diskFiles) == 0 {
		t.Fatal("dist 目录为空，请先执行 pnpm build")
	}

	var missing []string
	for _, name := range diskFiles {
		if _, err := distFS.Open(name); err != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("embed 缺失 %d 个产物文件（Go embed 默认忽略下划线/点开头文件，请使用 //go:embed all:dist）: %v",
			len(missing), missing)
	}
}
