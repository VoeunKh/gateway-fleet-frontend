package domain

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	uciSection = regexp.MustCompile(`^config [a-z_]+( '[^']*')?$`)
	uciOption  = regexp.MustCompile(`^\s+(option|list) [a-z_0-9]+ '[^']*'$`)
)

// ValidateUCI checks the restricted UCI syntax used by templates.
//
// sample: validateUci(text)
func ValidateUCI(text string) error {
	for i, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) == "" || uciSection.MatchString(l) || uciOption.MatchString(l) {
			continue
		}
		return invalid(fmt.Sprintf("Line %d is not valid UCI: \"%s\". Use config, option or list with values in single quotes.",
			i+1, strings.TrimSpace(l)))
	}
	if !strings.Contains(text, "config gateway 'main'") {
		return invalid("The config must keep the section config gateway 'main'.")
	}
	return nil
}

// ValidateConfigVersion checks a new template version before saving.
//
// sample: cfgsave — validateUci; 'Add a short note about what changed.'; 'Nothing changed from the latest version.'
func ValidateConfigVersion(text, note, latest string) error {
	if err := ValidateUCI(text); err != nil {
		return err
	}
	if strings.TrimSpace(note) == "" {
		return invalid("Add a short note about what changed.")
	}
	if text == latest {
		return invalid("Nothing changed from the latest version.")
	}
	return nil
}
