package browser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLockProfileRefusesASecondClaim(t *testing.T) {
	profile := t.TempDir()
	first, err := LockProfile(profile)
	if err != nil {
		t.Fatalf("first LockProfile() error = %v", err)
	}
	if _, err := LockProfile(profile); !errors.Is(err, ErrProfileLocked) {
		t.Fatalf("second LockProfile() error = %v, want ErrProfileLocked", err)
	}
	first.Release()
	again, err := LockProfile(profile)
	if err != nil {
		t.Fatalf("LockProfile() after release error = %v", err)
	}
	again.Release()
}

func TestClaimProfileGivesALaterInstanceAPrivateDirectory(t *testing.T) {
	profile := t.TempDir()
	lock, dataDir, err := ClaimProfile(profile)
	if err != nil {
		t.Fatalf("ClaimProfile() error = %v", err)
	}
	defer lock.Release()
	if lock == nil || dataDir != profile {
		t.Fatalf("first ClaimProfile() = (%v, %q), want the profile itself", lock, dataDir)
	}
	second, private, err := ClaimProfile(profile)
	if err != nil {
		t.Fatalf("second ClaimProfile() error = %v", err)
	}
	if second != nil {
		t.Fatalf("second ClaimProfile() took the profile lock")
	}
	if private == profile || private == "" {
		t.Fatalf("second ClaimProfile() dataDir = %q, want a private directory", private)
	}
	if info, err := os.Stat(private); err != nil || !info.IsDir() {
		t.Fatalf("private directory %q not created: %v", private, err)
	}
	(&OwnedBrowser{Profile: profile, DataDir: private}).Release()
	if _, err := os.Stat(private); !os.IsNotExist(err) {
		t.Fatalf("private directory %q survived release: %v", private, err)
	}
	if _, err := os.Stat(profile); err != nil {
		t.Fatalf("shared profile removed by a private release: %v", err)
	}
}

func TestClaimProfileSidestepsAviarysRunningBrowser(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Chromium's profile lock is a symlink only on Unix")
	}
	profile := t.TempDir()
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(profile, "SingletonLock")
	if err := os.Symlink(fmt.Sprintf("%s-%d", host, os.Getpid()), link); err != nil {
		t.Fatal(err)
	}
	lock, dataDir, err := ClaimProfile(profile)
	if err != nil {
		t.Fatalf("ClaimProfile() error = %v", err)
	}
	if lock != nil || dataDir == profile {
		t.Fatalf("ClaimProfile() = (%v, %q), want a private directory beside the running browser", lock, dataDir)
	}
	(&OwnedBrowser{Profile: profile, DataDir: dataDir}).Release()

	// A lock left by a browser that has exited is Chromium's to take over.
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(host+"-999999999", link); err != nil {
		t.Fatal(err)
	}
	lock, dataDir, err = ClaimProfile(profile)
	if err != nil {
		t.Fatalf("ClaimProfile() error = %v", err)
	}
	defer lock.Release()
	if lock == nil || dataDir != profile {
		t.Fatalf("ClaimProfile() with a stale lock = (%v, %q), want the profile itself", lock, dataDir)
	}
}

// TestCheckProfileFreeRefusesAProfileAnOwnedBrowserHolds checks the detached
// browser is not launched onto a profile another run's owned browser holds,
// where Chrome would hand the launch over and the wait would only time out.
func TestCheckProfileFreeRefusesAProfileAnOwnedBrowserHolds(t *testing.T) {
	profile := t.TempDir()
	if err := checkProfileFree(profile); err != nil {
		t.Fatalf("checkProfileFree() on a free profile error = %v", err)
	}
	lock, err := LockProfile(profile)
	if err != nil {
		t.Fatalf("LockProfile() error = %v, want the check to have released its claim", err)
	}
	defer lock.Release()
	if err := checkProfileFree(profile); !errors.Is(err, ErrProfileLocked) {
		t.Fatalf("checkProfileFree() on a held profile error = %v, want ErrProfileLocked", err)
	}
	if err := checkProfileFree(filepath.Join(profile, "missing")); err != nil {
		t.Fatalf("checkProfileFree() on a missing profile error = %v", err)
	}
}
