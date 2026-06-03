package service

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
	"unicode/utf16"
)

// xmlEscape escapes a value for inclusion in XML text/attribute content.
func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// taskXML builds a Windows Task Scheduler 1.2 definition for the per-user agent.
// It starts the agent at logon (LogonTrigger) and keeps it alive with a
// TimeTrigger that fires every minute indefinitely; MultipleInstancesPolicy
// IgnoreNew means a fire is a no-op while the agent is already running and a
// relaunch once it has exited — so a killed agent returns within ~1 minute. The
// TimeTrigger (rather than a repetition on the logon trigger) stays active while
// logged on regardless of when the task was registered, so a mid-session install
// has a live keepalive immediately. ExecutionTimeLimit PT0S removes the default
// 72-hour limit. Elevated maps to HighestAvailable, else LeastPrivilege.
func taskXML(s LaunchSpec) string {
	runLevel := "LeastPrivilege"
	if s.Elevated {
		runLevel = "HighestAvailable"
	}
	quoted := make([]string, 0, len(s.Args))
	for _, a := range s.Args {
		quoted = append(quoted, `"`+a+`"`)
	}
	user := xmlEscape(s.User)
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>vmrc VM remote-control agent (user session)</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>%[1]s</UserId>
    </LogonTrigger>
    <TimeTrigger>
      <Enabled>true</Enabled>
      <StartBoundary>2020-01-01T00:00:00</StartBoundary>
      <Repetition>
        <Interval>PT1M</Interval>
        <StopAtDurationEnd>false</StopAtDurationEnd>
      </Repetition>
    </TimeTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>%[1]s</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>%[2]s</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%[3]s</Command>
      <Arguments>%[4]s</Arguments>
    </Exec>
  </Actions>
</Task>`, user, runLevel, xmlEscape(s.Executable), xmlEscape(strings.Join(quoted, " ")))
}

// utf16LEBOM encodes s as UTF-16 little-endian with a byte-order mark, the
// encoding Task Scheduler expects for an XML definition passed to `schtasks
// /Create /XML`. (Contrast agent.json, where Go's JSON reader rejects a BOM.)
func utf16LEBOM(s string) []byte {
	codes := utf16.Encode([]rune(s))
	buf := bytes.NewBuffer(make([]byte, 0, 2+2*len(codes)))
	buf.Write([]byte{0xFF, 0xFE})
	for _, c := range codes {
		buf.WriteByte(byte(c))
		buf.WriteByte(byte(c >> 8))
	}
	return buf.Bytes()
}
