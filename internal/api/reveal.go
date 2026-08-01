package api

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
)

// RevealInExplorer opens the file manager pointing to the specified file.
func (a *App) RevealInExplorer(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", "/select,", path)
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	case "linux":
		// Many Linux file managers support selecting the file with xdg-open,
		// but standard xdg-open just opens the folder. We open the parent directory.
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return cmd.Start()
}
