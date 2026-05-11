package main

import (
	"os"
	"os/user"
	"strconv"
)

// handoffStateToService gives the state file to the `rcmd` system
// user (the one the systemd unit runs as) when the current process is
// root and that user exists. On non-deb installs (e.g.,
// `go run ./cmd/rcmdd init` on a dev box, where the system `rcmd`
// user is not present) this is a no-op.
//
// Without this, files written by `sudo rcmdd init` (or
// `sudo rcmdd rekey`) would be owned by root with mode 0600 and the
// service would fail to read them on startup.
func handoffStateToService(path string) {
	if os.Geteuid() != 0 {
		return
	}
	u, err := user.Lookup("rcmd")
	if err != nil {
		return
	}
	uid, err1 := strconv.Atoi(u.Uid)
	gid, err2 := strconv.Atoi(u.Gid)
	if err1 != nil || err2 != nil {
		return
	}
	_ = os.Chown(path, uid, gid)
	// 0640: owner (root) can rewrite, rcmd group can read.
	_ = os.Chmod(path, 0o640)
}
