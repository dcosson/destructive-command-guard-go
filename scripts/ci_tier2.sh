#!/bin/bash
set -euo pipefail

# Tier 2 (PR): D1-D3, F3-F4, O2, SEC2
# Runtime target: <30s

go test ./guard -run 'TestPropertyFuzzInvariantsTightness$' -count=1

go test ./internal/integration -run 'Test(DeterministicBenchmarkOrdering|DeterministicKnownMutationKillCoreGit|FaultGoldenFileMissingPack|FaultComparisonNoUpstreamBinary|OracleGoldenCrossValidation|SecurityGoldenFileNotExecuted|SecurityNoSubprocessExecutionWithEmptyPath)$' -count=1
