// Package logging configures structured logging for bpfman.
//
// It builds *slog.Logger instances over a spec string that sets a base
// level and optional per-component overrides, written as a
// comma-separated list such as "info,manager=debug,server=warn". A
// component is the value of the slog "component" attribute, so one spec
// can raise or lower the verbosity of individual subsystems without
// touching the rest.
//
// New constructs a logger from explicit Options; FromEnv and Default
// build one from the environment and from the built-in defaults
// respectively. ParseSpec turns a spec string into a Spec, ParseLevel
// and ParseFormat parse the scalar forms, and NewFilteringHandler wraps
// an slog.Handler so the per-component levels are applied at record
// time.
package logging
