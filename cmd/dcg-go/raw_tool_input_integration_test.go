//go:build integration

package main

import (
	"encoding/json"
	"testing"
)

func rawToolInput(t testing.TB, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal tool input: %v", err)
	}
	return data
}
