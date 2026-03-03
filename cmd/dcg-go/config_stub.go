package main

import "github.com/dcosson/destructive-command-guard-go/guard"

// config is implemented as a stub in o5w.3 and replaced by YAML loading in o5w.4.
type config struct{}

func loadConfig() config {
	return config{}
}

func (config) toOptions() []guard.Option {
	return nil
}
