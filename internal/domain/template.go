package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// SecretMask replaces secret values everywhere except the device payload.
const SecretMask = "••••••" // #nosec G101 -- a mask, not a credential

var (
	secretVar = regexp.MustCompile(`\{\{secret\.([a-z_]+)\}\}`)
	nonSlug   = regexp.MustCompile(`[^a-z0-9]+`)
)

// DefaultSite returns the sample's derived site values for a site name.
//
// sample: slug = d.site.toLowerCase().replace(/[^a-z0-9]+/g,'-'); ntp.${slug}.local; 'iot.carrier.apn'
func DefaultSite(name string) Site {
	slug := nonSlug.ReplaceAllString(strings.ToLower(name), "-")
	return Site{Name: name, NTPServer: "ntp." + slug + ".local", APN: "iot.carrier.apn"}
}

// RenderTemplate substitutes template variables for one device.
// With secrets == nil, secrets are masked (for UI, API and logs). With a map,
// real values are injected (device payload only) and a missing secret is an error.
//
// sample: render_vars(d, text)
func RenderTemplate(text string, sn string, site Site, secrets map[string]string) (string, error) {
	out := strings.NewReplacer(
		"{{device.sn}}", sn,
		"{{site.name}}", site.Name,
		"{{site.ntp_server}}", site.NTPServer,
		"{{site.apn}}", site.APN,
	).Replace(text)

	var missing []string
	out = secretVar.ReplaceAllStringFunc(out, func(m string) string {
		if secrets == nil {
			return SecretMask
		}
		key := secretVar.FindStringSubmatch(m)[1]
		v, ok := secrets[key]
		if !ok {
			missing = append(missing, key)
		}
		return v
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("render template: missing secrets %s", strings.Join(missing, ", "))
	}
	return out, nil
}
