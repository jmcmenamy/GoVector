package govec

import (
	"go.uber.org/zap/zapcore"
)

// GoLogCore wraps an existing zapcore.Core and intercepts writes, to duplicate writes
// to a GoLog logger. The interval GoLog zap logger does not use this struct
// Assumes this is used with NewTee, in order to make a multicore with this core and the baseCore
// This avoids needing to explicitly pass writes through to the base core
type GoLogCore struct {
	zapcore.Core
	gv *GoLog
}

type nopCore struct{ zapcore.Core }

// NewNopCore returns a no-op Core. Copy of zap's but enable returns true instead of false
func NewNopCore() zapcore.Core             { return nopCore{Core: zapcore.NewNopCore()} }
func (nopCore) Enabled(zapcore.Level) bool { return true }

// With adds structured context to the Core.
// Return copy of this core where fields will be added to every log to the GoLog and the base logger
func (c *GoLogCore) With(fields []zapcore.Field) zapcore.Core {
	return &GoLogCore{
		Core: c.Core.With(fields),
		gv:   c.gv,
	}
}

// Write entry with fields to this core, and pass write through to base wrapped core
// Write serializes the Entry and any Fields supplied at the log site and
// writes them to their destination.
//
// If called, Write should always log the Entry and Fields; it should not
// replicate the logic of Check.
func (c *GoLogCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	c.gv.mutex.Lock()
	defer c.gv.mutex.Unlock()
	fields, initialized := c.gv.addMetadataFields(entry, fields)
	if !initialized {
		return nil
	}
	return c.Core.Write(entry, fields)
}

// Have to overwrite method and copy implementation so our GoLogZapCore receiver is used instead of zapcore.ioCore
// when adding a core to a checkedEntry
// Check determines whether the supplied Entry should be logged (using the
// embedded LevelEnabler and possibly some extra logic). If the entry
// should be logged, the Core adds itself to the CheckedEntry and returns
// the result.
//
// Callers must use Check before calling Write.
func (c *GoLogCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}
