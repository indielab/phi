// Package commands owns the TUI slash/palette command surface.
//
// Assembly:
//
//	builtins := commands.NewBuiltinRegistry(bus, ctrl, composer, ...)
//	builtins.Bind(submitter, commandCtx, openPicker, streamActive)
//
// Domains (session, settings, extensions, skills, diff) register themselves and
// hold Ctrl/Bus/Composer directly — no *Deps / *Params bags.
package commands
