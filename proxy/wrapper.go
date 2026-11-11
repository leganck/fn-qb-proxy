package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"

	"github.com/sirupsen/logrus"
)

const (
	qbLauncherPath = "/usr/trim/bin/trim-qbittorrent-nox"
	qbRealPath     = qbLauncherPath + ".real"
	wrapperMarker  = "# fn-qb-proxy-wrapper"
)

const wrapperScript = `#!/bin/sh
# fn-qb-proxy-wrapper
base=/usr/trim/var/downloadcenter/$(id -u)/qb-proxy
mkdir -p "$base"
chmod 755 "$base" 2>/dev/null || true
for arg in "$@"; do
  case "$arg" in
    --webui-password=*)
      password=${arg#--webui-password=}
      case "$password" in
        ""|*\** ) ;;
        *)
          umask 077
          printf %s "$password" > "$base/password.tmp"
          chmod 600 "$base/password.tmp"
          mv -f "$base/password.tmp" "$base/password"
          ;;
      esac
      ;;
  esac
done
exec /usr/trim/bin/trim-qbittorrent-nox.real "$@"
`

func wrapperChecksum() string {
	sum := sha256.Sum256([]byte(wrapperScript))
	return hex.EncodeToString(sum[:])
}

func installQBWrapper() error {
	current, err := os.ReadFile(qbLauncherPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", qbLauncherPath, err)
	}
	if bytes.Equal(current, []byte(wrapperScript)) {
		if _, err := os.Stat(qbRealPath); err != nil {
			return fmt.Errorf("wrapper installed but original binary missing: %w", err)
		}
		return nil
	}

	if isELF(current) {
		if err := os.WriteFile(qbRealPath, current, 0755); err != nil {
			return fmt.Errorf("preserve original binary: %w", err)
		}
	} else if _, err := os.Stat(qbRealPath); err != nil {
		return fmt.Errorf("launcher is not the managed wrapper and original binary is missing")
	}

	if err := os.WriteFile(qbLauncherPath, []byte(wrapperScript), 0755); err != nil {
		return fmt.Errorf("install wrapper: %w", err)
	}
	logrus.Infof("Installed qBittorrent launcher wrapper at %s", qbLauncherPath)
	return nil
}

func uninstallQBWrapper() error {
	original, err := os.ReadFile(qbRealPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read original binary: %w", err)
	}
	if err := os.WriteFile(qbLauncherPath, original, 0755); err != nil {
		return fmt.Errorf("restore original launcher: %w", err)
	}
	if err := os.Remove(qbRealPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove original backup: %w", err)
	}
	logrus.Infof("Restored original qBittorrent launcher")
	return nil
}

func ensureQBWrapper() {
	before, err := os.ReadFile(qbLauncherPath)
	if err != nil {
		logrus.Warnf("Cannot check qBittorrent launcher: %v", err)
		return
	}
	if bytes.Equal(before, []byte(wrapperScript)) {
		return
	}
	if err := installQBWrapper(); err != nil {
		logrus.Errorf("Cannot repair qBittorrent launcher wrapper: %v", err)
		return
	}
	if err := exec.Command("systemctl", "restart", "dlcenter").Run(); err != nil {
		logrus.Warnf("Wrapper repaired, but restarting dlcenter failed: %v", err)
		return
	}
	logrus.Info("Repaired qBittorrent launcher wrapper and restarted dlcenter")
}

func isELF(data []byte) bool {
	return len(data) >= 4 && bytes.Equal(data[:4], []byte{0x7f, 'E', 'L', 'F'})
}
