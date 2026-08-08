package migration

import (
	"errors"
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    Command
		wantErr bool
	}{
		{name: "up", args: []string{"up"}, want: Command{Action: ActionUp}},
		{name: "version", args: []string{"version"}, want: Command{Action: ActionVersion}},
		{name: "down", args: []string{"down", "2"}, want: Command{Action: ActionDown, Version: 2}},
		{name: "force", args: []string{"force", "5"}, want: Command{Action: ActionForce, Version: 5}},
		{name: "case insensitive", args: []string{"UP"}, want: Command{Action: ActionUp}},
		{name: "missing action", wantErr: true},
		{name: "down requires steps", args: []string{"down"}, wantErr: true},
		{name: "down requires positive steps", args: []string{"down", "0"}, wantErr: true},
		{name: "unknown", args: []string{"redo"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCommand(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseCommand(%v) returned nil error", tt.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCommand(%v) returned error: %v", tt.args, err)
			}
			if got != tt.want {
				t.Fatalf("ParseCommand(%v) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

func TestParseCommandHelp(t *testing.T) {
	_, err := ParseCommand([]string{"help"})
	if !errors.Is(err, ErrHelp) {
		t.Fatalf("ParseCommand(help) error = %v, want ErrHelp", err)
	}
}

func TestMySQLURL(t *testing.T) {
	if got := mysqlURL("root:secret@tcp(localhost:3306)/chronoflow"); got != "mysql://root:secret@tcp(localhost:3306)/chronoflow" {
		t.Fatalf("mysqlURL() = %q", got)
	}
	if got := mysqlURL("mysql://root:secret@tcp(localhost:3306)/chronoflow"); got != "mysql://root:secret@tcp(localhost:3306)/chronoflow" {
		t.Fatalf("mysqlURL() = %q", got)
	}
}

func TestFileSourceURL(t *testing.T) {
	got, err := fileSourceURL("migrations")
	if err != nil {
		t.Fatalf("fileSourceURL returned error: %v", err)
	}
	if got[:7] != "file://" {
		t.Fatalf("fileSourceURL() = %q, want file URL", got)
	}
}
