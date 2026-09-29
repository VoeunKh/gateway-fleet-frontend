package domain

import "testing"

func TestDefaultSite(t *testing.T) {
	s := DefaultSite("Phnom Penh DC-2")
	if s.NTPServer != "ntp.phnom-penh-dc-2.local" || s.APN != "iot.carrier.apn" || s.Name != "Phnom Penh DC-2" {
		t.Errorf("%+v", s)
	}
}

func TestRenderTemplate(t *testing.T) {
	site := DefaultSite("Depot A")
	text := "sn '{{device.sn}}' site '{{site.name}}' ntp '{{site.ntp_server}}' apn '{{site.apn}}' key '{{secret.lora_key}}' {{unknown.var}}"
	tests := []struct {
		name    string
		secrets map[string]string
		want    string
		err     bool
	}{
		{"masked", nil,
			"sn 'GW1' site 'Depot A' ntp 'ntp.depot-a.local' apn 'iot.carrier.apn' key '••••••' {{unknown.var}}", false},
		{"injected", map[string]string{"lora_key": "s3cr3t"},
			"sn 'GW1' site 'Depot A' ntp 'ntp.depot-a.local' apn 'iot.carrier.apn' key 's3cr3t' {{unknown.var}}", false},
		{"missing secret", map[string]string{}, "", true},
	}
	for _, tc := range tests {
		got, err := RenderTemplate(text, "GW1", site, tc.secrets)
		if (err != nil) != tc.err || got != tc.want {
			t.Errorf("%s: got %q err=%v", tc.name, got, err)
		}
	}
}
