package envdetect

import (
	"testing"
)

func TestDetectInline_ExactVars(t *testing.T) {
	d := NewDetector()

	tests := []struct {
		name     string
		env      map[string]string
		wantProd bool
	}{
		{
			name:     "RAILS_ENV=production",
			env:      map[string]string{"RAILS_ENV": "production"},
			wantProd: true,
		},
		{
			name:     "RAILS_ENV=prod",
			env:      map[string]string{"RAILS_ENV": "prod"},
			wantProd: true,
		},
		{
			name:     "NODE_ENV=production case insensitive",
			env:      map[string]string{"NODE_ENV": "Production"},
			wantProd: true,
		},
		{
			name:     "FLASK_ENV=production",
			env:      map[string]string{"FLASK_ENV": "production"},
			wantProd: true,
		},
		{
			name:     "APP_ENV=prod",
			env:      map[string]string{"APP_ENV": "prod"},
			wantProd: true,
		},
		{
			name:     "MIX_ENV=prod",
			env:      map[string]string{"MIX_ENV": "prod"},
			wantProd: true,
		},
		{
			name:     "RACK_ENV=production",
			env:      map[string]string{"RACK_ENV": "production"},
			wantProd: true,
		},
		{
			name:     "ENVIRONMENT=production",
			env:      map[string]string{"ENVIRONMENT": "production"},
			wantProd: true,
		},
		{
			name:     "RAILS_ENV=development is not prod",
			env:      map[string]string{"RAILS_ENV": "development"},
			wantProd: false,
		},
		{
			name:     "RAILS_ENV=staging is not prod",
			env:      map[string]string{"RAILS_ENV": "staging"},
			wantProd: false,
		},
		{
			name:     "empty env",
			env:      map[string]string{},
			wantProd: false,
		},
		{
			name:     "nil env",
			env:      nil,
			wantProd: false,
		},
		{
			name:     "unrelated env var",
			env:      map[string]string{"HOME": "/home/user"},
			wantProd: false,
		},
		{
			name:     "lowercase key still matches",
			env:      map[string]string{"rails_env": "production"},
			wantProd: true,
		},
		{
			name:     "RAILS_ENV with whitespace",
			env:      map[string]string{"RAILS_ENV": " production "},
			wantProd: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := d.DetectInline(tt.env)
			if r.IsProduction != tt.wantProd {
				t.Errorf("DetectInline() = %v, want %v", r.IsProduction, tt.wantProd)
			}
			if r.IsProduction && len(r.Indicators) == 0 {
				t.Error("production detected but no indicators")
			}
			if r.IsProduction {
				for _, ind := range r.Indicators {
					if ind.Source != "inline" {
						t.Errorf("expected source=inline, got %q", ind.Source)
					}
				}
			}
		})
	}
}

func TestDetectInline_URLVars(t *testing.T) {
	d := NewDetector()

	tests := []struct {
		name     string
		env      map[string]string
		wantProd bool
	}{
		{
			name:     "DATABASE_URL with prod hostname",
			env:      map[string]string{"DATABASE_URL": "postgres://user:pass@prod-db.example.com:5432/mydb"},
			wantProd: true,
		},
		{
			name:     "DATABASE_URL with production hostname",
			env:      map[string]string{"DATABASE_URL": "postgres://user:pass@production-db.example.com:5432/mydb"},
			wantProd: true,
		},
		{
			name:     "DATABASE_URL with dev hostname",
			env:      map[string]string{"DATABASE_URL": "postgres://user:pass@dev-db.example.com:5432/mydb"},
			wantProd: false,
		},
		{
			name:     "REDIS_URL with prod",
			env:      map[string]string{"REDIS_URL": "redis://prod-redis.example.com:6379"},
			wantProd: true,
		},
		{
			name:     "MONGO_URL with prod",
			env:      map[string]string{"MONGO_URL": "mongodb://prod.example.com:27017/mydb"},
			wantProd: true,
		},
		{
			name:     "DATABASE_URL localhost",
			env:      map[string]string{"DATABASE_URL": "postgres://localhost:5432/mydb"},
			wantProd: false,
		},
		{
			name:     "DATABASE_URL not a URL",
			env:      map[string]string{"DATABASE_URL": "production"},
			wantProd: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := d.DetectInline(tt.env)
			if r.IsProduction != tt.wantProd {
				t.Errorf("DetectInline() = %v, want %v", r.IsProduction, tt.wantProd)
			}
		})
	}
}

func TestDetectInline_ProfileVars(t *testing.T) {
	d := NewDetector()

	tests := []struct {
		name     string
		env      map[string]string
		wantProd bool
	}{
		{
			name:     "AWS_PROFILE=prod",
			env:      map[string]string{"AWS_PROFILE": "prod"},
			wantProd: true,
		},
		{
			name:     "AWS_PROFILE=production",
			env:      map[string]string{"AWS_PROFILE": "production"},
			wantProd: true,
		},
		{
			name:     "AWS_PROFILE=my-prod-account",
			env:      map[string]string{"AWS_PROFILE": "my-prod-account"},
			wantProd: true,
		},
		{
			name:     "AWS_PROFILE=dev",
			env:      map[string]string{"AWS_PROFILE": "dev"},
			wantProd: false,
		},
		{
			name:     "GOOGLE_CLOUD_PROJECT=prod-project",
			env:      map[string]string{"GOOGLE_CLOUD_PROJECT": "prod-project"},
			wantProd: true,
		},
		{
			name:     "AZURE_SUBSCRIPTION=production-sub",
			env:      map[string]string{"AZURE_SUBSCRIPTION": "production-sub"},
			wantProd: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := d.DetectInline(tt.env)
			if r.IsProduction != tt.wantProd {
				t.Errorf("DetectInline() = %v, want %v", r.IsProduction, tt.wantProd)
			}
		})
	}
}

func TestDetectProcess(t *testing.T) {
	d := NewDetector()

	tests := []struct {
		name     string
		env      []string
		wantProd bool
	}{
		{
			name:     "RAILS_ENV=production",
			env:      []string{"RAILS_ENV=production"},
			wantProd: true,
		},
		{
			name:     "NODE_ENV=prod",
			env:      []string{"PATH=/usr/bin", "NODE_ENV=prod", "HOME=/home/user"},
			wantProd: true,
		},
		{
			name:     "no production vars",
			env:      []string{"PATH=/usr/bin", "HOME=/home/user"},
			wantProd: false,
		},
		{
			name:     "empty env",
			env:      nil,
			wantProd: false,
		},
		{
			name:     "malformed entry skipped",
			env:      []string{"NOEQUALSSIGN"},
			wantProd: false,
		},
		{
			name:     "value with equals",
			env:      []string{"DATABASE_URL=postgres://prod-db.example.com:5432/mydb?sslmode=require"},
			wantProd: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := d.DetectProcess(tt.env)
			if r.IsProduction != tt.wantProd {
				t.Errorf("DetectProcess() = %v, want %v", r.IsProduction, tt.wantProd)
			}
			if r.IsProduction {
				for _, ind := range r.Indicators {
					if ind.Source != "process" {
						t.Errorf("expected source=process, got %q", ind.Source)
					}
				}
			}
		})
	}
}

func TestDetectExported(t *testing.T) {
	d := NewDetector()

	tests := []struct {
		name     string
		exported map[string][]string
		wantProd bool
	}{
		{
			name:     "exported RAILS_ENV=production",
			exported: map[string][]string{"RAILS_ENV": {"production"}},
			wantProd: true,
		},
		{
			name:     "exported with multiple values, one is prod",
			exported: map[string][]string{"NODE_ENV": {"development", "production"}},
			wantProd: true,
		},
		{
			name:     "no prod values",
			exported: map[string][]string{"NODE_ENV": {"development", "test"}},
			wantProd: false,
		},
		{
			name:     "empty",
			exported: nil,
			wantProd: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := d.DetectExported(tt.exported)
			if r.IsProduction != tt.wantProd {
				t.Errorf("DetectExported() = %v, want %v", r.IsProduction, tt.wantProd)
			}
			if r.IsProduction {
				for _, ind := range r.Indicators {
					if ind.Source != "export" {
						t.Errorf("expected source=export, got %q", ind.Source)
					}
				}
			}
		})
	}
}

func TestMergeResults(t *testing.T) {
	a := Result{IsProduction: false}
	b := Result{
		IsProduction: true,
		Indicators:   []ProductionIndicator{{Source: "inline", Var: "RAILS_ENV", Value: "prod"}},
	}

	merged := MergeResults(a, b)
	if !merged.IsProduction {
		t.Error("merged should be production")
	}
	if len(merged.Indicators) != 1 {
		t.Errorf("expected 1 indicator, got %d", len(merged.Indicators))
	}

	// Both false
	merged = MergeResults(Result{}, Result{})
	if merged.IsProduction {
		t.Error("should not be production")
	}

	// Both true
	c := Result{
		IsProduction: true,
		Indicators:   []ProductionIndicator{{Source: "process", Var: "NODE_ENV", Value: "production"}},
	}
	merged = MergeResults(b, c)
	if !merged.IsProduction {
		t.Error("should be production")
	}
	if len(merged.Indicators) != 2 {
		t.Errorf("expected 2 indicators, got %d", len(merged.Indicators))
	}
}

func TestIsExactProd(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"production", true},
		{"prod", true},
		{"Production", true},
		{"PRODUCTION", true},
		{"PROD", true},
		{" prod ", true},
		{"development", false},
		{"staging", false},
		{"test", false},
		{"productive", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isExactProd(tt.input)
			if got != tt.want {
				t.Errorf("isExactProd(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestHasURLProd(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"prod in hostname", "postgres://user@prod-db.example.com/db", true},
		{"production in hostname", "redis://production.cache.example.com:6379", true},
		{"dev hostname", "postgres://dev-db.example.com/db", false},
		{"localhost", "postgres://localhost:5432/db", false},
		{"not a URL with prod", "production", true},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasURLProd(tt.url)
			if got != tt.want {
				t.Errorf("hasURLProd(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestHasProfileProd(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"prod", true},
		{"production", true},
		{"my-prod-account", true},
		{"prod-west", true},
		{"dev", false},
		{"staging", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := hasProfileProd(tt.input)
			if got != tt.want {
				t.Errorf("hasProfileProd(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
