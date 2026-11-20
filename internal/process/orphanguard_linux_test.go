package process

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestGuardOrphanedProcessSetsParentDeathSignal(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	guardOrphanedProcess(cmd)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Pdeathsig != syscall.SIGKILL {
		t.Fatalf("owned child was not given a parent-death signal")
	}
}
