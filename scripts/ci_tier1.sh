#!/bin/bash
set -euo pipefail

# Tier 1 (commit): P4, P5, D4
# Runtime target: <5s

go test ./e2etest -run 'Test(GoldenDecisionFileSelfConsistency|PropertyComparisonClassificationDeterministicExtended|DeterministicGoldenCorpusSize)$' -count=1
