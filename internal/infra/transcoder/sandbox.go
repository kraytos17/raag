package transcoder

import (
	"context"
	"os/exec"
	"syscall"
)

// sandboxCmd applies process-level sandboxing to an ffmpeg command:
//   - a completely empty environment (no inherited secrets/config),
//   - a hard 30s timeout via context (runaway prevention),
//   - Linux: Pdeathsig so a hung child dies with the parent.
//
// Network access is not possible because no transports are configured; the
// empty environment also prevents ffmpeg from loading user config that could
// enable network features. Full seccomp confinement is out of scope for the
// LAN-only build.
func sandboxCmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
	return cmd
}
