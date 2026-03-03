package parse

import (
	"context"
	"sync"
	"testing"
)

func TestConcurrentSafety100Goroutines(t *testing.T) {
	t.Parallel()

	parser := NewBashParser()
	inputs := []string{
		"echo hello",
		"git push --force origin main",
		"DIR=/ && DIR=/tmp && rm -rf $DIR",
		`python -c "import os; os.system('rm -rf /')"`,
		"cat <<'EOF' | bash\nrm -rf /\nEOF",
		"eval rm -rf /",
		"A=1 || A=2; B=1 || B=2; rm -rf $A$B",
	}

	const goroutines = 128
	const iterations = 40

	var wg sync.WaitGroup
	errs := make(chan string, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				in := inputs[(id+i)%len(inputs)]
				res := parser.ParseAndExtract(context.Background(), in, 0)
				if len(res.Commands) == 0 && in != "" {
					errs <- "no commands extracted for non-empty input"
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
