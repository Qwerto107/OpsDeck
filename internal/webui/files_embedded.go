//go:build with_frontend

package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var content embed.FS

// Files 让发布程序无需额外的前端目录即可运行。
func Files() fs.FS {
	files, _ := fs.Sub(content, "dist")
	return files
}
