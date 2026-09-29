package domain

import "testing"

const goodUCI = `config gateway 'main'
	option serial '{{device.sn}}'
	option heartbeat '30'

config ntp
	list server 'pool.ntp.org'
`

func TestValidateUCI(t *testing.T) {
	tests := []struct {
		name, text, err string
	}{
		{"valid", goodUCI, ""},
		{"blank lines ok", "\n\nconfig gateway 'main'\n  \n", ""},
		{"space indent ok", "config gateway 'main'\n  option x_1 'y'", ""},
		{"section without name", "config gateway 'main'\nconfig system", ""},
		{"unquoted value", "config gateway 'main'\n\toption heartbeat 30",
			`Line 2 is not valid UCI: "option heartbeat 30". Use config, option or list with values in single quotes.`},
		{"option not indented", "config gateway 'main'\noption a 'b'",
			`Line 2 is not valid UCI: "option a 'b'". Use config, option or list with values in single quotes.`},
		{"uppercase section", "config Gateway 'main'",
			`Line 1 is not valid UCI: "config Gateway 'main'". Use config, option or list with values in single quotes.`},
		{"double quotes", "config gateway \"main\"",
			`Line 1 is not valid UCI: "config gateway "main"". Use config, option or list with values in single quotes.`},
		{"missing main", "config gateway 'other'\n\toption a 'b'", "The config must keep the section config gateway 'main'."},
		{"empty", "", "The config must keep the section config gateway 'main'."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateUCI(tc.text)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tc.err {
				t.Errorf("got %q, want %q", got, tc.err)
			}
		})
	}
}

func TestValidateConfigVersion(t *testing.T) {
	changed := goodUCI + "\tlist server 'time.google.com'\n"
	tests := []struct {
		name, text, note, err string
	}{
		{"ok", changed, "add ntp", ""},
		{"invalid first", "nope", "", `Line 1 is not valid UCI: "nope". Use config, option or list with values in single quotes.`},
		{"note required", changed, "   ", "Add a short note about what changed."},
		{"unchanged", goodUCI, "x", "Nothing changed from the latest version."},
	}
	for _, tc := range tests {
		err := ValidateConfigVersion(tc.text, tc.note, goodUCI)
		got := ""
		if err != nil {
			got = err.Error()
		}
		if got != tc.err {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.err)
		}
	}
}
