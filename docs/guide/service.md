**Service Installation**

Usage:

- **Install**: `aviary service install` — Installs and starts the Aviary service for the current OS (systemd on Linux, launchd on macOS, Windows Service via sc).
- **Uninstall**: `aviary service uninstall` — Stops and removes the installed service.
- **Status**: `aviary service status` — Shows whether the Aviary server is running.
- **Start**: `aviary service start` — Start the installed Aviary service.
- **Stop**: `aviary service stop` — Stop the installed Aviary service.

Notes:

- The installer scripts (`installer/install.sh` and `installer/install.ps1`) will prompt to install the service after installing the binary. Use `-y` on the POSIX installer or `-Yes` on PowerShell to skip the prompt and auto-install the service.
- System-level service installation may require elevated privileges (e.g., sudo) depending on your platform and destination paths.
- The implementation writes unit files to `/etc/systemd/system` on Linux and `/Library/LaunchDaemons` on macOS, and uses `sc.exe` on Windows.

If you need a different configuration (custom user, environment, or paths), install manually or adjust the generated unit/plist before starting the service.
