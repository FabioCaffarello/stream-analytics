package jetstream

import (
	"regexp"
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

var (
	subjectEventPattern   = regexp.MustCompile(`^[a-z0-9_]+(?:\.[a-z0-9_]+)*$`)
	subjectVersionPattern = regexp.MustCompile(`^v[1-9][0-9]*$`)
	subjectTokenPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

var allowedSubjectRoots = map[string]struct{}{
	"aggregation": {},
	"insights":    {},
	"marketdata":  {},
	"quarantine":  {},
}

// ValidateSubjectTaxonomy validates concrete publish subjects.
// Taxonomy is interpreted as {event}.{version}.{venue}.{instrument}, where
// event may contain "." segments used by the repo (e.g. marketdata.trade).
func ValidateSubjectTaxonomy(subject string) error {
	event, version, venue, instrument, err := splitSubjectTaxonomy(subject)
	if err != nil {
		return err
	}

	if !subjectEventPattern.MatchString(event) {
		return problem.Newf(problem.ValidationFailed, "subject event %q is invalid", event)
	}
	if !subjectVersionPattern.MatchString(version) {
		return problem.Newf(problem.ValidationFailed, "subject version %q must be v{int}", version)
	}
	if !subjectTokenPattern.MatchString(venue) {
		return problem.Newf(problem.ValidationFailed, "subject venue %q is invalid", venue)
	}
	if !subjectTokenPattern.MatchString(instrument) {
		return problem.Newf(problem.ValidationFailed, "subject instrument %q is invalid", instrument)
	}
	root := strings.Split(event, ".")[0]
	if _, ok := allowedSubjectRoots[root]; !ok {
		return problem.Newf(problem.ValidationFailed, "subject root %q is not allowed", root)
	}
	return nil
}

// ValidateSubjectPattern validates JetStream stream/filter subject patterns.
func ValidateSubjectPattern(pattern string) error {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return problem.New(problem.ValidationFailed, "subject pattern must not be empty")
	}
	if strings.ContainsAny(pattern, " \t\r\n") {
		return problem.Newf(problem.ValidationFailed, "subject pattern %q must not contain spaces", pattern)
	}

	tokens := strings.Split(pattern, ".")
	if len(tokens) < 2 {
		return problem.Newf(problem.ValidationFailed, "subject pattern %q must have at least 2 tokens", pattern)
	}
	if _, ok := allowedSubjectRoots[tokens[0]]; !ok {
		return problem.Newf(problem.ValidationFailed, "subject pattern root %q is not allowed", tokens[0])
	}

	hasWildcard := false
	for i, token := range tokens {
		if token == "" {
			return problem.Newf(problem.ValidationFailed, "subject pattern %q has an empty token", pattern)
		}
		switch token {
		case "*":
			hasWildcard = true
			continue
		case ">":
			hasWildcard = true
			if i != len(tokens)-1 {
				return problem.Newf(problem.ValidationFailed, "subject pattern %q has invalid > placement", pattern)
			}
			continue
		}
		if strings.ContainsAny(token, "*>") {
			return problem.Newf(problem.ValidationFailed, "subject pattern %q has invalid wildcard token %q", pattern, token)
		}
		if !subjectTokenPattern.MatchString(token) {
			return problem.Newf(problem.ValidationFailed, "subject pattern %q token %q is invalid", pattern, token)
		}
	}
	if hasWildcard {
		return nil
	}
	return ValidateSubjectTaxonomy(pattern)
}

func splitSubjectTaxonomy(subject string) (event, version, venue, instrument string, err error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return "", "", "", "", problem.New(problem.ValidationFailed, "subject must not be empty")
	}
	if strings.ContainsAny(subject, " \t\r\n*>") {
		return "", "", "", "", problem.Newf(problem.ValidationFailed, "subject %q must be concrete and without spaces", subject)
	}

	tokens := strings.Split(subject, ".")
	if len(tokens) < 4 {
		return "", "", "", "", problem.Newf(problem.ValidationFailed, "subject %q must have at least 4 tokens", subject)
	}
	for _, token := range tokens {
		if strings.TrimSpace(token) == "" {
			return "", "", "", "", problem.Newf(problem.ValidationFailed, "subject %q has empty token", subject)
		}
	}
	event = strings.Join(tokens[:len(tokens)-3], ".")
	version = tokens[len(tokens)-3]
	venue = tokens[len(tokens)-2]
	instrument = tokens[len(tokens)-1]
	return event, version, venue, instrument, nil
}
