#!/bin/bash
set -euo pipefail

# Tier 3 (nightly): P1-P3, F1-F2, S1-S2, B1-B2, SEC1
# Runtime target: <60m

go test ./guard -run 'TestPropertyFuzzInvariantsTightness$' -count=1

go test ./internal/e2etest -run 'Test(PropertyMutationOperatorsDiffer|FaultMutationHarnessIdentityMutation|BenchmarkStability|BenchmarkInfrastructureJSONRoundTrip|BenchmarkRegressionDetectionThreshold|StressConcurrentGoldenCorpusEvaluation|StressHighVolumeFuzzSeedRunner|StressSustainedLoadMemoryPressure|StressMutationTimeLimit|SecurityFuzzCorpusClean)$' -count=1

go test ./guard -run '^$' -bench 'Benchmark(EvaluateThroughputMatrix|EvaluateOptionOverhead)' -benchtime=1x -count=1

go test ./cmd/dcg-go -run '^$' -bench 'Benchmark(HookJSONRoundtrip|ProcessHookInput)' -benchtime=1x -count=1
