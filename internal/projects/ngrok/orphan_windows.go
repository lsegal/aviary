package ngrok

import "errors"

// Windows needs no sweep. Job objects take every child down with glorp's own
// process object, so an agent cannot outlive the run that started it, and
// Windows offers no equivalent of Unix reparenting to pid 1 — the recorded
// parent id of a process whose parent has exited is stale rather than a marker
// of ownership, so there is no safe way to tell an abandoned agent from one a
// live glorp still owns.
func platformRunningProcesses() ([]processEntry, error) { return nil, nil }

func platformTerminateProcess(int) error { return errors.New("unsupported on windows") }
