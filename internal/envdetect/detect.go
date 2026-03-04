package envdetect

import (
	"net/url"
	"regexp"
	"strings"
)

// Detector checks environment variables for production indicators.
type Detector struct {
	exactVars   []string
	urlVars     []string
	profileVars []string
}

// Result holds the outcome of environment detection.
type Result struct {
	IsProduction bool
	Indicators   []ProductionIndicator
}

// ProductionIndicator records why a production environment was detected.
type ProductionIndicator struct {
	Source string // "inline", "export", "process"
	Var    string // environment variable name
	Value  string // environment variable value
}

// prodWordBoundary matches "prod" or "production" as whole words.
var prodWordBoundary = regexp.MustCompile(`(?i)\bprod(?:uction)?\b`)

// exactProdVars are environment variables checked for exact "prod"/"production" values.
var exactProdVars = []string{
	"RAILS_ENV",
	"NODE_ENV",
	"FLASK_ENV",
	"APP_ENV",
	"MIX_ENV",
	"RACK_ENV",
	"ENVIRONMENT",
}

// urlProdVars are environment variables whose URL hostnames are checked for "prod".
var urlProdVars = []string{
	"DATABASE_URL",
	"REDIS_URL",
	"MONGO_URL",
	"ELASTICSEARCH_URL",
}

// profileProdVars are environment variables checked for "prod" as a word boundary.
var profileProdVars = []string{
	"AWS_PROFILE",
	"GOOGLE_CLOUD_PROJECT",
	"AZURE_SUBSCRIPTION",
}

// NewDetector creates a Detector with the standard production detection rules.
func NewDetector() *Detector {
	return &Detector{
		exactVars:   exactProdVars,
		urlVars:     urlProdVars,
		profileVars: profileProdVars,
	}
}

// DetectInline checks inline environment variables (set with the command).
func (d *Detector) DetectInline(inlineEnv map[string]string) Result {
	return d.detect(inlineEnv, "inline")
}

// DetectProcess checks process-level environment variables (KEY=VALUE format).
func (d *Detector) DetectProcess(processEnv []string) Result {
	envMap := parseProcessEnv(processEnv)
	return d.detect(envMap, "process")
}

// DetectExported checks exported variables from shell script analysis.
func (d *Detector) DetectExported(exportedVars map[string][]string) Result {
	var result Result
	for key, values := range exportedVars {
		for _, val := range values {
			r := d.detectSingle(key, val, "export")
			if r.IsProduction {
				result.IsProduction = true
				result.Indicators = append(result.Indicators, r.Indicators...)
			}
		}
	}
	return result
}

// MergeResults combines two Results. Production is true if either is true.
func MergeResults(a, b Result) Result {
	r := Result{
		IsProduction: a.IsProduction || b.IsProduction,
	}
	if len(a.Indicators)+len(b.Indicators) > 0 {
		r.Indicators = make([]ProductionIndicator, 0, len(a.Indicators)+len(b.Indicators))
		r.Indicators = append(r.Indicators, a.Indicators...)
		r.Indicators = append(r.Indicators, b.Indicators...)
	}
	return r
}

func (d *Detector) detect(env map[string]string, source string) Result {
	var result Result
	for key, val := range env {
		r := d.detectSingle(key, val, source)
		if r.IsProduction {
			result.IsProduction = true
			result.Indicators = append(result.Indicators, r.Indicators...)
		}
	}
	return result
}

func (d *Detector) detectSingle(key, value, source string) Result {
	upperKey := strings.ToUpper(key)

	// Check exact-value env vars.
	for _, ev := range d.exactVars {
		if upperKey == ev && isExactProd(value) {
			return Result{
				IsProduction: true,
				Indicators: []ProductionIndicator{
					{Source: source, Var: key, Value: value},
				},
			}
		}
	}

	// Check URL-shaped env vars.
	for _, uv := range d.urlVars {
		if upperKey == uv && hasURLProd(value) {
			return Result{
				IsProduction: true,
				Indicators: []ProductionIndicator{
					{Source: source, Var: key, Value: value},
				},
			}
		}
	}

	// Check profile env vars.
	for _, pv := range d.profileVars {
		if upperKey == pv && hasProfileProd(value) {
			return Result{
				IsProduction: true,
				Indicators: []ProductionIndicator{
					{Source: source, Var: key, Value: value},
				},
			}
		}
	}

	return Result{}
}

// isExactProd checks if value is exactly "prod" or "production" (case-insensitive).
func isExactProd(value string) bool {
	v := strings.TrimSpace(strings.ToLower(value))
	return v == "production" || v == "prod"
}

// hasURLProd parses a URL and checks hostname for "prod" as a word boundary.
func hasURLProd(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" {
		// Fall back to checking the raw value if it's not a valid URL.
		return prodWordBoundary.MatchString(value)
	}
	host := u.Hostname()
	return prodWordBoundary.MatchString(host)
}

// hasProfileProd checks for "prod" as a word boundary in the value.
func hasProfileProd(value string) bool {
	return prodWordBoundary.MatchString(value)
}

// parseProcessEnv converts KEY=VALUE slice to a map.
func parseProcessEnv(envSlice []string) map[string]string {
	m := make(map[string]string, len(envSlice))
	for _, kv := range envSlice {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			continue
		}
		m[kv[:idx]] = kv[idx+1:]
	}
	return m
}
