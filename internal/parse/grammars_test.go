package parse

import (
	"context"
	"testing"
)

func TestSupportedLanguagesIncludeBash(t *testing.T) {
	t.Parallel()

	var found bool
	for _, grammar := range SupportedLanguages {
		if grammar.Name == "bash" {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("supported languages does not include bash")
	}
}

func TestNewLangParserParsesSource(t *testing.T) {
	t.Parallel()

	samples := map[string]string{
		"bash":       "echo hello",
		"python":     "print('hello')\n",
		"ruby":       "puts 'hello'\n",
		"javascript": "console.log('hello');\n",
		"perl":       "print \"hello\\n\";\n",
		"lua":        "print('hello')\n",
	}

	for _, grammar := range SupportedLanguages {
		grammar := grammar
		t.Run(grammar.Name, func(t *testing.T) {
			t.Parallel()

			lp := NewLangParser(grammar)
			tree := lp.Parse(context.Background(), []byte(samples[grammar.Name]))
			if tree == nil {
				t.Fatalf("parse returned nil tree for language %q", grammar.Name)
			}
		})
	}
}
