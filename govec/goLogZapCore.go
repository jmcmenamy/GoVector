package govec

import "go.uber.org/zap/zapcore"

// GoLogZapCore wraps an existing zapcore.Core and intercepts writes, to duplicate writes
// to a GoLog logger
type GoLogZapCore struct {
	zapcore.Core
	gv     *GoLog
	fields []zapcore.Field
}

// With adds structured context to the Core.
// Return copy of this core where fields will be added to every log to the GoLog and the base logger
func (c *GoLogZapCore) With(fields []zapcore.Field) zapcore.Core {
	return &GoLogZapCore{
		Core:   c.Core.With(fields),
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
func (c *GoLogZapCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	zapFields := fields
	if c.fields != nil {
		zapFields = append(fields, c.fields...)
	}
	c.gv.logLocalEntryZap(entry, zapFields)
	// Proceed with writing the log entry as normal.
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
func (c *GoLogZapCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

// Sync flushes buffered logs (if any).
// Also Syncs the base wrapped core
func (c *GoLogZapCore) Sync() error {
	err := c.Core.Sync()
	if err != nil {
		return err
	}
	return c.gv.writeSyncer.Sync()
}
