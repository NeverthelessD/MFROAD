package main

import (
	"os/exec"
	"runtime"
	"syscall"
)

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// detachSelf moves this process into its own session, so it keeps running when the launcher ends.
func detachSelf() { syscall.Setsid() }

// openPaths opens files/folders with their default app (Finder · 미리보기 …).
func openPaths(paths []string) {
	if runtime.GOOS == "darwin" && len(paths) > 0 {
		exec.Command("/usr/bin/open", paths...).Start()
	}
}

// revealPaths shows the files selected in Finder.
func revealPaths(paths []string) {
	if runtime.GOOS == "darwin" && len(paths) > 0 {
		exec.Command("/usr/bin/open", append([]string{"-R"}, paths...)...).Start()
	}
}
