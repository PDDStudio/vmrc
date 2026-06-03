package service

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestTaskXMLElevated(t *testing.T) {
	spec := LaunchSpec{
		Name:       "vmrc-agent",
		Executable: `C:\Users\dev\AppData\Local\vmrc\bin\vmrc-agent.exe`,
		Args:       []string{"run", "--config", `C:\Users\dev\AppData\Local\vmrc\agent.json`},
		User:       `DESKTOP-1\dev`,
		Elevated:   true,
	}
	xml := taskXML(spec)
	for _, want := range []string{
		`<LogonTrigger>`,
		`<TimeTrigger>`,
		`<Interval>PT1M</Interval>`,
		`<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>`,
		`<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>`,
		`<StartWhenAvailable>true</StartWhenAvailable>`,
		`<RunLevel>HighestAvailable</RunLevel>`,
		`<UserId>DESKTOP-1\dev</UserId>`,
		`<Command>C:\Users\dev\AppData\Local\vmrc\bin\vmrc-agent.exe</Command>`,
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("taskXML missing %q in:\n%s", want, xml)
		}
	}
	if !strings.Contains(xml, `&#34;run&#34;`) || !strings.Contains(xml, `&#34;--config&#34;`) {
		t.Fatalf("expected quoted+escaped args, got:\n%s", xml)
	}
}

func TestTaskXMLLeastPrivilegeWhenNotElevated(t *testing.T) {
	xml := taskXML(LaunchSpec{User: `D\u`, Executable: `C:\x.exe`, Args: []string{"run"}})
	if !strings.Contains(xml, `<RunLevel>LeastPrivilege</RunLevel>`) {
		t.Fatalf("want LeastPrivilege, got:\n%s", xml)
	}
}

func TestTaskXMLWellFormed(t *testing.T) {
	doc := taskXML(LaunchSpec{
		Executable: `C:\path with space\vmrc-agent.exe`,
		Args:       []string{"run", "--config", `C:\x\agent.json`},
		User:       `DESKTOP-1\dev`,
		Elevated:   true,
	})
	dec := xml.NewDecoder(strings.NewReader(doc))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("taskXML is not well-formed XML: %v\n%s", err, doc)
		}
	}
}

func TestUTF16LEBOM(t *testing.T) {
	got := utf16LEBOM("AB")
	want := []byte{0xFF, 0xFE, 'A', 0x00, 'B', 0x00}
	if !bytes.Equal(got, want) {
		t.Fatalf("utf16LEBOM(\"AB\") = % x, want % x", got, want)
	}
}
