package govec

import (
	"sync"
	"time"

	"go.uber.org/zap/zapcore"
)

// GoLogWriteSyncer lets you flip between an unbuffered and a buffered write syncer.
type GoLogWriteSyncer struct {
	mu           sync.RWMutex
	unbuf        zapcore.WriteSyncer
	buf          *zapcore.BufferedWriteSyncer
	manualBuffer bool

	// always == unbuf or buf, under mu
	active zapcore.WriteSyncer
}

func newGoLogWriteSyncer(unbuf zapcore.WriteSyncer) *GoLogWriteSyncer {
	return &GoLogWriteSyncer{
		unbuf:  unbuf,
		buf:    nil,
		active: unbuf,
	}
}

func (s *GoLogWriteSyncer) Write(p []byte) (int, error) {
	s.mu.RLock()
	w := s.active
	s.mu.RUnlock()
	return w.Write(p)
}

func (s *GoLogWriteSyncer) Sync() error {
	s.mu.RLock()
	w := s.active
	s.mu.RUnlock()
	return w.Sync()
}

// EnableBuffering switches over to the buffered writer
// size is the maximum amount of data the writer will buffered before flushing.
// interval is how often the writer should flush data if there have been no writes. 0 if only manual Syncing
func (s *GoLogWriteSyncer) EnableBuffering(size int, interval time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// no op if already buffering
	if s.active == s.buf {
		return nil
	}
	s.buf = &zapcore.BufferedWriteSyncer{
		WS:            s.unbuf,
		Size:          size,
		FlushInterval: interval,
	}
	s.manualBuffer = interval == 0
	if s.manualBuffer {
		// Write a no-op to make sure this BufferedWriteSyncer has been initialized and flush loop started, so it will correctly stop it
		_, err := s.buf.Write([]byte{})
		if err != nil {
			return err
		}
		err = s.buf.Stop()
		if err != nil {
			return err
		}
	}
	s.active = s.buf
	return nil
}

// DisableBuffering switches back to the unbuffered writer
// Syncs any data in the existing buf
func (s *GoLogWriteSyncer) DisableBuffering() (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// no op if already not buffering
	if s.active == s.unbuf {
		return nil
	}
	// flush any buffered data before disabling
	err = s.buf.Stop()
	if err != nil {
		return err
	}
	s.active = s.unbuf
	// if manual, stop is a no op since we previously stopped, so sync manually
	if s.manualBuffer {
		err = s.buf.Sync()
	}
	s.buf = nil
	return err
}
