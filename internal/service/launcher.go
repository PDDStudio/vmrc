package service

// LaunchSpec describes the autostart unit a Launcher manages: a per-user agent
// that runs the given executable with args at logon, in the user's session.
type LaunchSpec struct {
	Name       string
	Executable string
	Args       []string
	User       string
	Elevated   bool
}

// Launcher manages a per-user autostart unit (Windows logon Scheduled Task or a
// kardianos UserService on macOS/Linux). Same verbs as the service Manager.
type Launcher interface {
	Install() error
	Uninstall() error
	Start() error
	Stop() error
	Status() (string, error) // "running" | "stopped" | "unknown"
}

// NewLauncher returns the OS-appropriate per-user autostart Launcher.
func NewLauncher(spec LaunchSpec) (Launcher, error) { return newOSLauncher(spec) }
