package govec

import (
	"bytes"
	"errors"

	"go.uber.org/zap/zapcore"
)

// GoLogWriteSyncer wraps a zapcore.WriteSyncer
// It is used to support buffering in Zap
type GoLogWriteSyncer struct {
	baseWriteSyncer zapcore.WriteSyncer
	buffer          *bytes.Buffer
	gv              *GoLog
}

// writes p to the internal buffer or directly to the wrapped WriteSyncer if not buffering.
func (ws *GoLogWriteSyncer) Write(p []byte) (int, error) {
	if ws.gv.buffered.Load() {
		return ws.buffer.Write(p)
	}
	return ws.baseWriteSyncer.Write(p)
}

// flushes the buffered data and syncs the file.
// throws an error if the baseWriteSyncer hasn't been configured yet
func (ws *GoLogWriteSyncer) Sync() error {
	if ws.baseWriteSyncer == nil {
		// TODO add an actual err here to return
		ws.gv.Error("Sync called before WriteSyncer initialized")
		return errors.New("Sync() called on GoLogWriteSyncer without baseWriteSyncer initialized")
	}
	if _, err := ws.buffer.WriteTo(ws.baseWriteSyncer); err != nil {
		return err
	}
	return ws.baseWriteSyncer.Sync()
}
