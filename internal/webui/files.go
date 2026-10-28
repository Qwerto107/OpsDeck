//go:build !with_frontend

package webui

import (
	"io/fs"
	"os"
)

// Files 在源码开发模式下读取前端构建目录。
func Files() fs.FS { return os.DirFS("web/dist") }
