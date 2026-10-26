package service

import "strings"

// systemdRestartSec is the delay before systemd relaunches the agent after it
// exits. kardianos' built-in unit hard-codes RestartSec=120, which turned every
// remote /update (the agent exits so its supervisor relaunches the new binary)
// and every crash into a two-minute outage. Five seconds still keeps a crash
// loop well below StartLimitBurst (10 starts within StartLimitInterval=5s).
const systemdRestartSec = "5"

// systemdUnitTemplate is kardianos/service v1.3.0's built-in systemd template
// (rendered by its mini template engine, passed via the "SystemdScript"
// option), with two changes:
//
//   - RestartSec is systemdRestartSec instead of 120;
//   - a user unit (systemctl --user) is WantedBy=default.target. The built-in
//     multi-user.target does not exist in a user manager, so an enabled user
//     unit was never pulled in at login (or at boot under linger).
//
// Everything else is kept verbatim so the unit stays what kardianos renders.
// Note: no KillMode is set, so systemd's default (control-group) applies and
// any process the agent spawned (/spawn) is stopped together with the agent on
// restart, update, or `vmrc-agent stop`.
const systemdUnitTemplate = `[Unit]
Description={{Description}}
ConditionFileIsExecutable={{Path | cmdEscape}}
{{range Dependencies}}{{.}}
{{end}}
[Service]
StartLimitInterval=5
StartLimitBurst=10
ExecStart={{Path | cmdEscape}}{{range Arguments}} {{. | cmd}}{{end}}
{{if ChRoot}}RootDirectory={{ChRoot | cmd}}
{{end}}{{if WorkingDirectory}}WorkingDirectory={{WorkingDirectory | cmdEscape}}
{{end}}{{if UserName}}User={{UserName}}
{{end}}{{if ReloadSignal}}ExecReload=/bin/kill -{{ReloadSignal}} "$MAINPID"
{{end}}{{if PIDFile}}PIDFile={{PIDFile | cmd}}
{{end}}{{if OutputFileSupport}}StandardOutput=file:{{LogDirectory}}/{{Name}}.out
StandardError=file:{{LogDirectory}}/{{Name}}.err
{{end}}{{if LimitNOFILE}}LimitNOFILE={{LimitNOFILE}}
{{end}}{{if Restart}}Restart={{Restart}}
{{end}}{{if SuccessExitStatus}}SuccessExitStatus={{SuccessExitStatus}}
{{end}}RestartSec=@RESTARTSEC@
EnvironmentFile=-/etc/sysconfig/{{Name}}

{{range EnvVars}}{{.}}
{{end}}[Install]
WantedBy=@WANTEDBY@
`

// systemdUnit returns the SystemdScript for a system (multi-user.target) or
// user (default.target) unit. Ignored by kardianos on non-systemd platforms.
func systemdUnit(user bool) string {
	wantedBy := "multi-user.target"
	if user {
		wantedBy = "default.target"
	}
	return strings.NewReplacer("@RESTARTSEC@", systemdRestartSec, "@WANTEDBY@", wantedBy).Replace(systemdUnitTemplate)
}
