package govec

import (
	"go.uber.org/zap/zapcore"
)

// GoLogWrappedZapCore wraps an existing zapcore.Core and intercepts writes, to duplicate writes
// to a GoLog logger. The interval GoLog zap logger does not use this struct
// Assumes this is used with NewTee, in order to make a multicore with this core and the baseCore
// This avoids needing to explicitly pass writes through to the base core
type GoLogWrappedZapCore struct {
	level  zapcore.Level
	core   zapcore.Core
	gv     *GoLog
	fields []zapcore.Field
}

// With adds structured context to the Core.
// Return copy of this core where fields will be added to every log to the GoLog and the base logger
func (c *GoLogWrappedZapCore) With(fields []zapcore.Field) zapcore.Core {
	return &GoLogWrappedZapCore{
		level:  c.level,
		core:   c.core,
		gv:     c.gv,
		fields: append(c.fields, fields...),
	}
}

// Write entry with fields to this core, and pass write through to base wrapped core
// Write serializes the Entry and any Fields supplied at the log site and
// writes them to their destination.
//
// If called, Write should always log the Entry and Fields; it should not
// replicate the logic of Check.
func (c *GoLogWrappedZapCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	zapFields := fields
	if c.fields != nil {
		zapFields = append(fields, c.fields...)
	}
	return c.gv.logLocalEntryZap(entry, zapFields)
}

// Have to overwrite method and copy implementation so our GoLogZapCore receiver is used instead of zapcore.ioCore
// when adding a core to a checkedEntry
// Check determines whether the supplied Entry should be logged (using the
// embedded LevelEnabler and possibly some extra logic). If the entry
// should be logged, the Core adds itself to the CheckedEntry and returns
// the result.
//
// Callers must use Check before calling Write.
func (c *GoLogWrappedZapCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

// Enabled returns true if the given level is at or above this level.
// If our level is zapcore.InvalidLevel, default to the level of the wrapped core
func (c *GoLogWrappedZapCore) Enabled(level zapcore.Level) bool {
	if c.level != zapcore.InvalidLevel {
		return c.level.Enabled(level)
	}
	return c.core.Enabled(level)
}

// Sync flushes buffered logs (if any).
func (c *GoLogWrappedZapCore) Sync() error {
	return c.gv.Sync()
}
