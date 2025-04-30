// Package govec is a vector clock logging library
package govec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DistributedClocks/GoVector/govec/vclock"
	ct "github.com/daviddengcn/go-colortext"
	"github.com/vmihailenco/msgpack/v5"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	logToTerminal                       = true
	_             msgpack.CustomEncoder = (*vclock.VClockPayload)(nil)
	_             msgpack.CustomDecoder = (*vclock.VClockPayload)(nil)
)

// colorLookup is the array of status to string from runtime/proc.go
var colorLookup = map[zapcore.Level]ct.Color{
	zapcore.DebugLevel: ct.Green,
	zapcore.InfoLevel:  ct.White,
	zapcore.WarnLevel:  ct.Yellow,
	zapcore.ErrorLevel: ct.Red,
	zapcore.FatalLevel: ct.Magenta,
}

// prefixLookup translates priority enums into strings
var prefixLookup = map[zapcore.Level]string{
	zapcore.DebugLevel: "DEBUG",
	zapcore.InfoLevel:  "INFO",
	zapcore.WarnLevel:  "WARNING",
	zapcore.ErrorLevel: "ERROR",
	zapcore.FatalLevel: "FATAL",
}

// GoLogConfig controls the logging parameters of GoLog and is taken as
// input to GoLog initialization. See defaults in GetDefaultConfig.
type GoLogConfig struct {
	// Buffered denotes if the logging events are buffered until flushed. This option
	// increase logging performance at the cost of safety.
	Buffered bool
	// PrintOnScreen denotes if logging events are printed to screen.
	PrintOnScreen bool
	// AppendLog determines to continue writing to a log from a prior execution.
	AppendLog bool
	// UseTimestamps determines to log real time timestamps for TSVis
	UseTimestamps bool
	// EncodingStrategy for customizable interoperability
	EncodingStrategy func(interface{}) ([]byte, error)
	// DecodingStrategy for customizable interoperability
	DecodingStrategy func([]byte, interface{}) error
	// LogToFile determines to write logging events to a file
	LogToFile bool
	// Priority determines the minimum priority event to log
	// Priority LogPriority
	Level zapcore.Level

	AddCaller     bool
	AddStacktrace bool
	// When buffering, the maximum amount of data the writer will buffer before flushing
	Size int

	// When buffering, how often the writer should flush data if there have been no writes. 0 if only manual Syncing
	Interval time.Duration
	// InitialVC is the initial vector clock value, nil by default
	InitialVC vclock.VClock
	// Whether or not to generate logs with zap, which supports arbitrary key value pairs
	GenerateZapLogs bool
	// Whether or not to generate logs using regex style, compatible with the original Shiviz
	GenerateRegexLogs bool
	// Prefix to add to zap log paths for where to write files. Empty is fine
	ZapLogPrefix string
}

// GetDefaultConfig returns the default GoLogConfig with default values
// for various fields.
func GetDefaultConfig() GoLogConfig {
	config := GoLogConfig{
		Buffered:          false,
		PrintOnScreen:     false,
		AppendLog:         false,
		UseTimestamps:     false,
		LogToFile:         true,
		Level:             zapcore.InfoLevel,
		InitialVC:         nil,
		GenerateZapLogs:   false,
		GenerateRegexLogs: true,
		AddCaller:         true,
		AddStacktrace:     true,
	}
	return config
}

// GetDefaultZapConfig returns the default GoLogConfig with default values
// for various fields when generating logs with Zap
func GetDefaultZapConfig() GoLogConfig {
	config := GetDefaultConfig()
	config.GenerateRegexLogs = false
	config.GenerateZapLogs = true
	return config
}

// GoLogOptions provides logging parameters for individual logging statements
type GoLogOptions struct {
	// The Log priority for this event
	Priority zapcore.Level
}

// GetDefaultLogOptions returns the default GoLogOptions with default values
// for the fields
func GetDefaultLogOptions() GoLogOptions {
	o := GoLogOptions{Priority: zapcore.InfoLevel}
	return o
}

// SetPriority returns a new GoLogOptions object with its priority field
// set to Priority. Follows the builder pattern.
// Priority : (GoLogPriority) The Priority that the new GoLogOptions object must have
func (o *GoLogOptions) SetPriority(Priority zapcore.Level) GoLogOptions {
	opts := *o
	opts.Priority = Priority
	return opts
}

// Represents an entry that we log later, once the zap logger is initialized
type ZapEntryInput struct {
	entry  zapcore.Entry
	fields []zapcore.Field
}

// Represents a log that we try to log later, once the zap logger is initialized
type ZapLogInput struct {
	level  zapcore.Level
	msg    string
	fields []zap.Field
}

// GoLog struct provides an interface to creating and maintaining
// vector timestamp entries in the generated log file. GoLogs are
// initialized with Configs which control logger output, format, and
// frequency.
type GoLog struct {

	// Local Process ID
	pid string

	// Local vector clock in bytes
	currentVC vclock.VClock

	// Flag to Printf the logs made by Local Program
	printonscreen bool

	// If true GoLog will write to a file
	logging bool

	// If logging and true GoLog will write to a file with regex style logs
	loggingRegex bool

	// if logging and true GoLog will write to a file with zap style logs
	loggingZap bool

	// If true logs are buffered in memory and flushed to disk upon
	// calling flush. Logs can be lost if a program stops prior to
	// flushing buffered logs.
	buffered *atomic.Bool

	// When buffering, the maximum amount of data the writer will buffer before flushing
	size int

	// When buffering, how often the writer should flush data if there have been no writes. 0 if only manual Syncing
	interval time.Duration

	// Flag to include timestamps when logging events
	usetimestamps bool

	// Flag to indicate if the log file will contain multiple executions
	appendLog bool

	// Flag to indicate if the message broadcast is on
	broadcast bool

	// Priority level at which all events are logged
	// priority LogPriority
	level zapcore.Level

	addCaller     bool
	addStacktrace bool

	// Logfile name
	logfile string

	// buffered string
	output                   string
	goLogWriteSyncer         *GoLogWriteSyncer
	goLogCore                *GoLogCore
	preInitializationEntries []*ZapEntryInput
	preInitializationLogs    []*ZapLogInput

	// encoding and decoding strategies for network messages
	encodingStrategy func(interface{}) ([]byte, error)
	decodingStrategy func([]byte, interface{}) error

	// Internal internalLogger for printing errors
	internalLogger *log.Logger

	mutex *sync.RWMutex

	*zap.Logger
	initialized        *atomic.Bool
	SugaredLogger      *zap.SugaredLogger
	wrappedLogger      *zap.Logger
	wrappedLoggerTwice *zap.Logger
	// wrappedLoggerThrice *zap.Logger
	zapLogPrefix string
}

func (gv *GoLog) updateLoggers(baseLogger *zap.Logger) {
	gv.Logger = baseLogger
	gv.SugaredLogger = gv.Logger.Sugar()
	gv.wrappedLogger = gv.Logger.WithOptions(zap.AddCallerSkip(1))
	gv.wrappedLoggerTwice = gv.Logger.WithOptions(zap.AddCallerSkip(2))
	// gv.wrappedLoggerThrice = gv.Logger.WithOptions(zap.AddCallerSkip(3))
}

// Given an existing zap Logger, return a new zap logger that will
// keep the same configuration, but also write any logs to this GoLog logger
// the level of the core of this logger will be the level of gv
func (gv *GoLog) WrapBaseZapLogger(baseLogger *zap.Logger, opts ...zap.Option) *zap.Logger {
	opts = append(opts, zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		// could use zapcore.NewTee here but still would need to embed this core
		return zapcore.NewTee(core, gv.Logger.Core())
	}))
	return baseLogger.WithOptions(opts...)
}

// prepareGoLogWriteSyncer opens the filePaths in append mode (or creates it if it doesn't exist),
// and sets the writeSyncer of gv to a GoLogWriteSyncer safe for concurrent use.
func (gv *GoLog) prepareGoLogWriteSyncer(filePaths []string) error {
	// Ensure the directory for each file path exists
	for _, path := range filePaths {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			// Handle the error appropriately
			log.Fatalf("failed to create directory %q: %v", dir, err)
		}
	}
	baseWriteSyncer, _, err := zap.Open(filePaths...)
	if err != nil {
		gv.internalLogger.Printf("Gott err making write Syncer %v prefix %v\n", filePaths, gv.zapLogPrefix)
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Current working directory:", cwd)
	gv.internalLogger.Printf("Successfully make go log write syncer with filepaths %v and prefix %v\n", filePaths, gv.zapLogPrefix)
	goLogWriteSyncer := newGoLogWriteSyncer(baseWriteSyncer)
	if gv.buffered.Load() {
		goLogWriteSyncer.EnableBuffering(gv.size, gv.interval)
	}
	gv.goLogWriteSyncer = goLogWriteSyncer
	return nil
}

// Creates an Uninitialized GoLog that can still be logged to with zap logs
// Loggs are buffered and will be logged immediately once this logger is initialized
// and output paths are given
func UninitializedGoVector() *GoLog {
	goLog := &GoLog{logging: true, loggingZap: true, level: zapcore.InfoLevel, mutex: &sync.RWMutex{}, buffered: &atomic.Bool{}, initialized: &atomic.Bool{}}
	if logToTerminal {
		goLog.internalLogger = log.New(os.Stdout, "[GoVector]:", log.Lshortfile)
		goLog.internalLogger.Printf("success?\n")

	} else {
		goLog.internalLogger = log.New(io.Discard, "[GoVector]:", log.Lshortfile)
	}
	goLogCore := &GoLogCore{Core: NewNopCore(), gv: goLog}
	goLog.initialized.Store(false)
	goLog.updateLoggers(zap.New(goLogCore, zap.AddCaller(), zap.AddStacktrace(goLog.level)))
	goLog.goLogCore = goLogCore
	return goLog
}

// InitGoVector returns a GoLog which generates a logs prefixed with
// processid, to a file name logfilename.log. Any old log with the same
// name will be trucated. Config controls logging options. See GoLogConfig for more details.
func InitGoVector(processid string, logfilename string, config GoLogConfig) *GoLog {
	gv := UninitializedGoVector()
	gv.InitGoVector(processid, config, logfilename)
	return gv
}

// // Used to update the level of this logger
// // Essentially a no op if this logger has already been initialized
// // Useful if a specific level for logging is desired for wrapping a base logger with WrapBaseZapLogger
// func (gv *GoLog) UpdateLevel(level zapcore.Level) {
// 	gv.mutex.Lock()
// 	defer gv.mutex.Unlock()
// 	gv.level = level
// }

// InitGoVector returns a GoLog which generates a logs prefixed with
// processid, to files with names logfilenames, appended with a -Log.txt or -zap-Log.txt. Any old log with the same
// name will be truncated for non-zap logs. zap logs will be parsed and used to initialize the vector clock
// Config controls logging options. See GoLogConfig for more details.
func (gv *GoLog) InitGoVector(processid string, config GoLogConfig, logfilenames ...string) {
	gv.mutex.Lock()
	// defer gv.mutex.Unlock()
	// gv := &GoLog{}
	gv.pid = processid
	if logToTerminal && gv.internalLogger == nil {
		gv.internalLogger = log.New(os.Stdout, "[GoVector]:", log.Lshortfile)
	} else if gv.internalLogger == nil {
		gv.internalLogger = log.New(io.Discard, "[GoVector]:", log.Lshortfile)
	}
	gv.internalLogger.Print("Just made internal logger\n")
	fmt.Printf("Printing here for pid %v\n", gv.pid)

	//Set parameters from config
	gv.printonscreen = config.PrintOnScreen
	gv.usetimestamps = config.UseTimestamps
	// gv.priority = config.Priority
	gv.level = config.Level
	gv.loggingRegex = config.LogToFile && config.GenerateRegexLogs
	gv.loggingZap = config.LogToFile && config.GenerateZapLogs
	gv.logging = gv.loggingRegex || gv.loggingZap

	gv.appendLog = config.AppendLog
	gv.output = ""

	// Use the default encoder/decoder. As of July 2017 this is msgPack.
	if config.EncodingStrategy == nil || config.DecodingStrategy == nil {
		gv.setEncoderDecoder(defaultEncoder, defaultDecoder)
	} else {
		gv.setEncoderDecoder(config.EncodingStrategy, config.DecodingStrategy)
	}

	// We create a new Vector Clock with processname and config.InitialClock
	// as the initial time
	var vc1 vclock.VClock
	if config.InitialVC == nil {
		vc1 = vclock.New()
		vc1.Set(processid, 0)
	} else {
		vc1 = config.InitialVC
	}
	gv.currentVC = vc1

	//Starting File IO . If Log exists, Log Will be deleted and A New one will be created
	if len(logfilenames) == 0 {
		gv.internalLogger.Printf("No file names passed in")
	}
	gv.logfile = logfilenames[0] + "-Log.txt"
	gv.zapLogPrefix = config.ZapLogPrefix
	if gv.loggingRegex {
		gv.prepareLogFile()
		gv.Flush()
	}
	if gv.loggingZap {
		for i, logfilename := range logfilenames {
			logfilenames[i] = logfilename + "-zap-Log.txt"
		}
		gv.buffered.Store(config.Buffered)
		gv.interval = config.Interval
		gv.size = config.Size
		gv.addCaller = config.AddCaller
		gv.addStacktrace = config.AddStacktrace
		gv.internalLogger.Printf("Preparing zap logger\n")
		err := gv.prepareZapLogger(logfilenames)
		gv.initialized.Store(true)
		if err != nil {
			gv.internalLogger.Printf("Got err creating zap logger: %v\n", err)
		}
		gv.mutex.Unlock()

		if len(gv.preInitializationLogs) > 0 {
			gv.LogLocalEventZap(zapcore.InfoLevel, "Going to log the backed up messages")
		}
		for _, zapLog := range gv.preInitializationLogs {
			gv.LogLocalEventZap(zapLog.level, zapLog.msg, zapLog.fields...)
		}
		if len(gv.preInitializationEntries) > 0 {
			gv.LogLocalEventZap(zapcore.InfoLevel, "Going to log the backed up entries")
		}
		for _, zapLog := range gv.preInitializationEntries {
			gv.Core().Write(zapLog.entry, zapLog.fields)
		}
		gv.Sync()
		gv.mutex.Lock()
	}
	gv.preInitializationLogs = nil
	gv.preInitializationEntries = nil
	gv.internalLogger.Printf("finished initializing for %v clock is %v\n", gv.pid, gv.currentVC.GetMap()[gv.pid])
	gv.mutex.Unlock()
	// gv.currentVC.Tick(gv.pid)
	// ok := gv.logThis("Initialization Complete", gv.pid, gv.currentVC.ReturnVCString(), gv.priority)
	// if !ok {
	// 	gv.logger.Println("Something went Wrong, Could not Log!")
	// }
}

// Assumes gv.logfilename has been set
// prepares gv.logfilename to be written to with regex style logs
func (gv *GoLog) prepareLogFile() {
	_, err := os.Stat(gv.logfile)
	if err == nil {
		if !gv.appendLog {
			gv.internalLogger.Println(gv.logfile, "exists! ... Deleting ")
			os.Remove(gv.logfile)
		} else {
			executionnumber := time.Now().Format(time.UnixDate)
			gv.internalLogger.Println("Execution Number is  ", executionnumber)
			executionstring := "=== Execution #" + executionnumber + "  ==="
			gv.logThis(executionstring, "", "", gv.level)
			return
		}
	}
	// Create directory path to log if it doesn't exist.
	if err := os.MkdirAll(filepath.Dir(gv.logfile), 0750); err != nil {
		gv.internalLogger.Println(err)
	}

	//Creating new Log
	file, err := os.Create(gv.logfile)
	if err != nil {
		gv.internalLogger.Println(err)
	}

	file.Close()

	if gv.appendLog {
		executionnumber := time.Now().Format(time.UnixDate)
		gv.internalLogger.Println("Execution Number is  ", executionnumber)
		executionstring := "=== Execution #" + executionnumber + "  ==="
		gv.logThis(executionstring, "", "", gv.level)
	}

}

// seeks to the end of the file and reads backwards to extract the last line.
func readLastLine(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Get file size.
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	filesize := fi.Size()
	if filesize == 0 {
		return "", nil // Empty file.
	}

	// Start from the end.
	offset := filesize - 1

	// Skip any trailing newline characters.
	for offset >= 0 {
		b := make([]byte, 1)
		_, err := f.ReadAt(b, offset)
		if err != nil {
			return "", err
		}
		if b[0] != '\n' {
			break
		}
		offset--
	}

	// Now, find the beginning of the last line.
	start := offset
	for start >= 0 {
		b := make([]byte, 1)
		_, err := f.ReadAt(b, start)
		if err != nil {

			return "", err
		}
		if b[0] == '\n' {
			// The last line starts after this newline.
			start++
			break
		}
		start--
	}
	if start < 0 {
		start = 0 // The whole file is one line.
	}

	// Read from start to offset.
	length := offset - start + 1
	buf := make([]byte, length)
	_, err = f.ReadAt(buf, start)
	if err != nil && err != io.EOF {

		return "", err
	}

	return string(buf), nil
}

// attempts to create a new zap logger, writing to the given filePaths
// logger is a json logger including the caller, function, and stack trace for logs
// uses the first filePath in filePaths to try to initialize VCString to last output VCString
func (gv *GoLog) prepareZapLogger(filePaths []string) error {
	var firstFilePath string
	for _, filePath := range filePaths {
		adjustedPath := gv.zapLogPrefix + "/" + filePath
		_, err := os.Stat(adjustedPath)
		if err == nil {
			firstFilePath = adjustedPath
			break
		} else {
			gv.internalLogger.Printf("Got err in state for filePath %v for pid %v: %v\n", filePath, gv.pid, err.Error())
		}
	}
	// If the log file already exists, read and process its last line.
	gv.internalLogger.Printf("Preparing zap logger with first filename: %v for PID %v\n", firstFilePath, gv.pid)
	if firstFilePath != "" {
		lastLine, err := readLastLine(firstFilePath)
		if err != nil {
			gv.internalLogger.Printf("failed to read last line: %v\n", err.Error())
		}
		if err == nil && lastLine != "" {
			gv.internalLogger.Printf("Last line wasn't empty \n")
			var jsonObj map[string]interface{}
			if err := json.Unmarshal([]byte(lastLine), &jsonObj); err != nil {
				gv.internalLogger.Printf("Error parsing JSON: %v\n", err)
			} else {
				if vc, ok := jsonObj["VCString"]; ok {
					// gv.logger.Printf("VCString: %v\n", vc)
					// gv.logger.Printf("VCString type: %v\n", reflect.TypeOf(vc))
					VCMap := make(map[string]uint64)
					for k, v := range vc.(map[string]interface{}) {
						VCMap[k] = uint64(v.(float64))
					}
					gv.internalLogger.Printf("Merged clock was %v for pid %v\n", gv.currentVC.ReturnVCString(), gv.pid)
					gv.currentVC.Merge(VCMap)
					gv.internalLogger.Printf("Merged clock is now %v for pid %v\n", gv.currentVC.ReturnVCString(), gv.pid)
					// gv.preInitializationLogs = append(
					// 	gv.preInitializationLogs,
					// 	&ZapLogInput{
					// 		level:  gv.level,
					// 		msg:    "Boostrapped logger with existing file",
					// 		fields: []zap.Field{zap.String("filePath", firstFilePath)},
					// 	},
					// )
				} else {
					gv.internalLogger.Printf("VCString field not found in JSON\n")
				}
			}
		}
	}

	// Open the log file (zap handles file creation)
	if gv.zapLogPrefix != "" {
		for i := range filePaths {
			filePaths[i] = gv.zapLogPrefix + "/" + filePaths[i]
		}
	}
	if err := gv.prepareGoLogWriteSyncer(filePaths); err != nil {
		return err
	}

	// Configure encoder
	// encoderConfig := zap.NewDevelopmentEncoderConfig()
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:       "timestamp",                 // Include timestamp in JSON
		LevelKey:      "level",                     // Log level
		CallerKey:     "caller",                    // Caller location
		MessageKey:    "message",                   // Log message
		StacktraceKey: "stacktrace",                // Strack trace
		FunctionKey:   "function",                  // Function name
		EncodeTime:    zapcore.ISO8601TimeEncoder,  // Format timestamp
		EncodeLevel:   zapcore.CapitalLevelEncoder, // INFO, DEBUG, etc.
		EncodeCaller:  zapcore.ShortCallerEncoder,  // File:line
	}
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig), // Human-readable logs
		gv.goLogWriteSyncer,
		gv.level, // Log level
	)

	// safe to do since we're holding lock
	gv.goLogCore.Core = core

	// Create the logger
	opts := []zap.Option{}
	if gv.addCaller {
		opts = append(opts, zap.AddCaller())
	}
	if gv.addStacktrace {
		opts = append(opts, zap.AddStacktrace(gv.level))
	}
	gv.updateLoggers(gv.Logger.WithOptions(opts...))
	return nil
}

// GetCurrentVC returns the current vector clock
func (gv *GoLog) GetCurrentVC() vclock.VClock {
	return gv.currentVC
}

// setEncoderDecoder Sets the Encoding and Decoding functions which are to be used by the logger
// Encoder (func(interface{}) ([]byte, error)) : function to be used for encoding
// Decoder (func([]byte, interface{}) error) : function to be used for decoding
func (gv *GoLog) setEncoderDecoder(encoder func(interface{}) ([]byte, error), decoder func([]byte, interface{}) error) {
	gv.encodingStrategy = encoder
	gv.decodingStrategy = decoder
}

// By default, encoding is performed by msgpack
func defaultEncoder(payload interface{}) ([]byte, error) {
	return msgpack.Marshal(payload)
}

// By default, decoding network payloads is perfomed by msgpack
func defaultDecoder(buf []byte, payload interface{}) error {
	return msgpack.Unmarshal(buf, payload)
}

// EnableBufferedWrites enables buffered writes to the log file. All
// the log messages are only written to the LogFile via an explicit
// call to the function Flush.  Note: Buffered writes are automatically
// disabled.
func (gv *GoLog) EnableBufferedWrites() (err error) {
	gv.buffered.Store(true)
	if gv.goLogWriteSyncer != nil {
		err = gv.goLogWriteSyncer.EnableBuffering(gv.size, gv.interval)
	}
	return err
}

// DisableBufferedWrites disables buffered writes to the log file. All
// the log messages from now on will be written to the Log file
// immediately. Writes all the existing log messages that haven't been
// written to Log file yet.
func (gv *GoLog) DisableBufferedWrites() (err error) {
	gv.buffered.Store(false)
	// gv.buffered = false
	if gv.output != "" {
		gv.Flush()
	}
	if gv.goLogWriteSyncer != nil {
		err = gv.goLogWriteSyncer.DisableBuffering()
	}
	// sync any logs buffered in our GoLogWriteSyncer
	return err
}

// Flush writes the log messages stored in the buffer to the Log File.
// This function should be used by the application to also force writes
// in the case of interrupts and crashes.   Note: Calling Flush when
// BufferedWrites is disabled is essentially a no-op.
// Only to be used for regex style logging; no op for zap style logging
func (gv *GoLog) Flush() bool {
	complete := true
	file, err := os.OpenFile(gv.logfile, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		complete = false
	}
	if gv.output == "" {
		return true
	}
	// info, err := file.Stat()
	// name, err := filepath.Abs(file.Name())
	// gv.logger.Printf("Opened file %v %v\n", name, info)
	defer file.Close()

	if _, err = file.WriteString(gv.output); err != nil {
		complete = false
	}

	gv.output = ""
	return complete
}

func (gv *GoLog) printColoredMessage(LogMessage string, Priority zapcore.Level) {
	color := colorLookup[Priority]
	prefix := prefixLookup[Priority]
	ct.Foreground(color, true)
	fmt.Print(time.Now().Format(time.UnixDate) + ":" + prefix + "-")
	ct.ResetColor()
	fmt.Println(LogMessage)
}

// Logs a message along with a processID and a vector clock, the VCString
// must be a valid vector clock, true is returned on success. logThis
// is the innermost logging function internally used by all other
// logging functions
func (gv *GoLog) logThis(Message string, ProcessID string, VCString string, Priority zapcore.Level) bool {
	var (
		complete = true
		buffer   bytes.Buffer
	)

	if gv.loggingRegex {
		gv.tickClock()
		if gv.usetimestamps {
			buffer.WriteString(strconv.FormatInt(time.Now().UnixNano(), 10))
			buffer.WriteString(" ")
		}
		buffer.WriteString(ProcessID)
		buffer.WriteString(" ")
		buffer.WriteString(VCString)
		buffer.WriteString("\n")
		buffer.WriteString(Message)
		buffer.WriteString("\n")
		output := buffer.String()

		gv.output += output
		// gv.logger.Printf("HEY LOOK GOT HERE IN logThis %v\n", !gv.buffered)
		if !gv.buffered.Load() {
			complete = gv.Flush()
		}
	}

	if gv.printonscreen {
		gv.printColoredMessage(Message, Priority)
	}

	if gv.loggingZap {
		gv.mutex.Unlock()
		gv.Log(Priority, Message)
		gv.mutex.Lock()
	}
	return complete
}

// logWriteWrapper is a helper function for wrapping common logging
// behaviour associated with logThis
func (gv *GoLog) logWriteWrapper(mesg, errMesg string, priority zapcore.Level) (success bool) {
	// gv.logger.Printf("HEY LOOK logging this: %v\n", mesg)
	if gv.logging {
		prefix := prefixLookup[priority]
		wrappedMesg := prefix + " " + mesg
		success = gv.logThis(wrappedMesg, gv.pid, gv.currentVC.ReturnVCString(), priority)
		if !success {
			// gv.logger.Printf("HEY LOOK logging this cause of error: %v\n", errMesg)
			gv.internalLogger.Println(errMesg)
		}
	}
	return
}

// Increment GoVectors local clock by 1
func (gv *GoLog) tickClock() error {
	_, found := gv.currentVC.FindTicks(gv.pid)
	if !found {
		errMsg := "couldn't find this process's id in its own vector clock"
		gv.internalLogger.Println(errMsg)
		return errors.New(errMsg)
	}
	gv.currentVC.Tick(gv.pid)
	return nil
}

// LogLocalEvent implements LogLocalEvent with priority
// levels. If the priority of the logger is lower than or equal to the
// priority of this event then the current vector timestamp is
// incremented and the message is logged it into the Log File. A color
// coded string is also printed on the console.
// * LogMessage (string) : Message to be logged
// * Priority (LogPriority) : Priority at which the message is to be logged
func (gv *GoLog) LogLocalEvent(mesg string, opts GoLogOptions) (logSuccess bool) {
	logSuccess = true
	gv.mutex.Lock()
	if opts.Priority >= gv.level {
		// gv.tickClock()
		logSuccess = gv.logWriteWrapper(mesg, "Something went Wrong, Could not Log LocalEvent!", opts.Priority)
	}
	gv.mutex.Unlock()
	return
}

// Adds processId and VCString fields to a list of zap fields, tickClock
// If logger isn't initialized, saves entry for later logging
func (gv *GoLog) addMetadataFields(entry zapcore.Entry, fields []zapcore.Field) ([]zap.Field, bool) {
	initialized := gv.initialized.Load()
	if !initialized {
		gv.preInitializationEntries = append(gv.preInitializationEntries, &ZapEntryInput{entry: entry, fields: fields})
		return fields, initialized
	}
	err := gv.tickClock()
	if err != nil {
		gv.internalLogger.Printf("Error when adding metadata: %v", err)
	}
	if gv.usetimestamps {
		fields = append(fields,
			zap.String("processId", gv.pid),
			gv.currentVC.ReturnVCStringZap("VCString"),
			zap.String("timestamp", strconv.FormatInt(time.Now().UnixNano(), 10)),
		)
	} else {
		fields = append(fields,
			zap.String("processId", gv.pid),
			gv.currentVC.ReturnVCStringZap("VCString"),
		)
	}
	return fields, initialized
}

// Redefine Zap methods that return *zap.Logger, so we return GoLog instead

func (gv *GoLog) clone() *GoLog {
	clone := *gv
	return &clone
}

// Named adds a new path segment to the logger's name. Segments are joined by
// periods. By default, Loggers are unnamed.
func (gv *GoLog) Named(s string) *GoLog {
	if s == "" {
		return gv
	}
	l := gv.clone()
	l.updateLoggers(l.Logger.Named(s))
	return l
}

// WithOptions clones the current Logger, applies the supplied Options, and
// returns the resulting Logger. It's safe to use concurrently.
func (gv *GoLog) WithOptions(opts ...zap.Option) *GoLog {
	c := gv.clone()
	c.updateLoggers(c.Logger.WithOptions(opts...))
	return c
}

// With creates a child logger and adds structured context to it. Fields added
// to the child don't affect the parent, and vice versa. Any fields that
// require evaluation (such as Objects) are evaluated upon invocation of With.
func (gv *GoLog) With(fields ...zap.Field) *GoLog {
	if len(fields) == 0 {
		return gv
	}
	l := gv.clone()
	l.updateLoggers(l.Logger.With(fields...))
	return l
}

// WithLazy creates a child logger and adds structured context to it lazily.
//
// The fields are evaluated only if the logger is further chained with [With]
// or is written to with any of the log level methods.
// Until that occurs, the logger may retain references to objects inside the fields,
// and logging will reflect the state of an object at the time of logging,
// not the time of WithLazy().
//
// WithLazy provides a worthwhile performance optimization for contextual loggers
// when the likelihood of using the child logger is low,
// such as error paths and rarely taken branches.
//
// Similar to [With], fields added to the child don't affect the parent, and vice versa.
func (gv *GoLog) WithLazy(fields ...zap.Field) *GoLog {
	if len(fields) == 0 {
		return gv
	}
	return gv.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		return zapcore.NewLazyWith(core, fields)
	}))
}

// Assumes we are holding gv.mutex
// Ticks the clock and logs the given msg and fields and level level
// If the zap logger isn't initialized yet, just store it for logging later
// Returns whether we successfully passed the log to the logger. If false, the log should be stored for later when the log is initialized
// Assumes we are holding gv.mutex and are are logging and have a defined logger
func (gv *GoLog) LogLocalEventZap(level zapcore.Level, msg string, fields ...zap.Field) {
	if !gv.initialized.Load() {
		gv.preInitializationLogs = append(gv.preInitializationLogs, &ZapLogInput{level: level, msg: msg, fields: fields})
		return
	}
	gv.wrappedLogger.Log(level, msg, fields...)
}

// PrepareSend is meant to be used immediately before sending.
// LogMessage will be logged along with the time of send
// buf is encode-able data (structure or basic type)
// Returned is an encoded byte array with logging information.
// This function is meant to be called before sending a packet. Usually,
// it should Update the Vector Clock for its own process, package with
// the clock using gob support and return the new byte array that should
// be sent onwards using the Send Command
func (gv *GoLog) PrepareSend(mesg string, buf interface{}, opts GoLogOptions) (encodedBytes []byte) {
	//Converting Vector Clock from Bytes and Updating the gv clock
	// gv.logger.Printf("HEY LOOK GOT TO START OF PREPARE SEND with: %v\n", mesg)
	if !gv.broadcast {
		gv.mutex.Lock()
		// gv.logger.Printf("HEY LOOK HERE IN PREPARE SEND clock is %v for %v\n", gv.currentVC.ReturnVCString(), gv.logfile)
		if opts.Priority >= gv.level {
			// gv.tickClock()

			// gv.logger.Printf("In prepare send writing to %v\n", gv.logfile)
			gv.logWriteWrapper(mesg, "Something went wrong, could not log prepare send", opts.Priority)

			d := vclock.VClockPayload{Pid: gv.pid, VcMap: gv.currentVC.GetMap(), Payload: buf}

			// encode the Clock Payload
			var err error
			encodedBytes, err = gv.encodingStrategy(&d)
			if err != nil {
				gv.internalLogger.Println(err.Error())
			}

			// return encodedBytes which can be sent off and received on the other end!
		}
		gv.mutex.Unlock()

	} else {
		// Broadcast: do not acquire the lock, tick the clock or log an event as
		// all been done by StartBroadcast
		d := vclock.VClockPayload{Pid: gv.pid, VcMap: gv.currentVC.GetMap(), Payload: buf}

		var err error
		encodedBytes, err = gv.encodingStrategy(&d)
		if err != nil {
			gv.internalLogger.Println(err.Error())
		}
	}
	return
}

// PrepareSend is meant to be used immediately before sending.
// LogMessage will be logged along with the time of send
// buf is encode-able data (structure or basic type)
// Returned is an encoded byte array with logging information.
// This function is meant to be called before sending a packet. Usually,
// it should Update the Vector Clock for its own process, package with
// the clock using gob support and return the new byte array that should
// be sent onwards using the Send Command
func (gv *GoLog) PrepareSendZapWrapPayload(mesg string, buf interface{}, level zapcore.Level, fields ...zap.Field) (encodedBytes []byte) {
	//Converting Vector Clock from Bytes and Updating the gv clock
	// gv.logger.Printf("HEY LOOK GOT TO START OF PREPARE SEND with: %v\n", mesg)
	if !gv.broadcast {
		// gv.mutex.Lock()
		// gv.logger.Printf("HEY LOOK HERE IN PREPARE SEND clock is %v for %v\n", gv.currentVC.ReturnVCString(), gv.logfile)
		// if level >= gv.level {
		// gv.tickClock()

		// gv.logger.Printf("In prepare send writing to %v\n", gv.logfile)
		if buf == 0 {
			gv.wrappedLoggerTwice.Log(level, mesg, fields...)
		} else {
			gv.wrappedLogger.Log(level, mesg, fields...)
		}
		// gv.LogLocalEventZap(level, mesg, fields...)

		gv.mutex.Lock()

		d := vclock.VClockPayload{Pid: gv.pid, VcMap: gv.currentVC.GetMap(), Payload: buf}

		// encode the Clock Payload
		var err error
		encodedBytes, err = gv.encodingStrategy(&d)
		if err != nil {
			gv.internalLogger.Println(err.Error())
		}

		// return encodedBytes which can be sent off and received on the other end!
		// }
		gv.mutex.Unlock()

	} else {
		// Broadcast: do not acquire the lock, tick the clock or log an event as
		// all been done by StartBroadcast
		d := vclock.VClockPayload{Pid: gv.pid, VcMap: gv.currentVC.GetMap(), Payload: buf}

		var err error
		encodedBytes, err = gv.encodingStrategy(&d)
		if err != nil {
			gv.internalLogger.Println(err.Error())
		}
	}
	return
}

func (gv *GoLog) PrepareSendZap(mesg string, level zapcore.Level, fields ...zap.Field) (encodedBytes []byte) {
	return gv.PrepareSendZapWrapPayload(mesg, 0, level, fields...)
}

func (gv *GoLog) mergeIncomingClock(mesg string, e vclock.VClockPayload, Priority zapcore.Level) {
	// First, tick the local clock
	// gv.tickClock()
	gv.currentVC.Merge(e.VcMap)

	gv.logWriteWrapper(mesg, "Something went Wrong, Could not Log!", Priority)
}

// UnpackReceive is used to unmarshall network data into local structures.
// LogMessage will be logged along with the vector time receive happened
// buf is the network data, previously packaged by PrepareSend unpack is
// a pointer to a structure, the same as was packed by PrepareSend.
// This function is meant to be called immediately after receiving
// a packet. It unpacks the data by the program, the vector clock. It
// updates vector clock and logs it. and returns the user data
func (gv *GoLog) UnpackReceive(mesg string, buf []byte, unpack interface{}, opts GoLogOptions) {
	gv.mutex.Lock()

	if opts.Priority >= gv.level {
		e := vclock.VClockPayload{}
		e.Payload = unpack

		// Just use msgpack directly
		err := gv.decodingStrategy(buf, &e)
		if err != nil {
			gv.internalLogger.Println(err.Error())
		}

		// gv.logger.Printf("Merging incoming clocks for %v\n", gv.logfile)
		// Increment and merge the incoming clock
		gv.mergeIncomingClock(mesg, e, opts.Priority)
	}
	gv.mutex.Unlock()

}

// UnpackReceive is used to unmarshall network data into local structures.
// LogMessage will be logged along with the vector time receive happened
// buf is the network data, previously packaged by PrepareSend unpack is
// a pointer to a structure, the same as was packed by PrepareSend.
// This function is meant to be called immediately after receiving
// a packet. It unpacks the data by the program, the vector clock. It
// updates vector clock and logs it. and returns the user data
func (gv *GoLog) UnpackReceiveZapWrapPayload(mesg string, buf []byte, unpack interface{}, level zapcore.Level, fields ...zap.Field) {
	gv.unpackReceiveZapWrapPayload(mesg, buf, unpack, level, 1, fields)
}

func (gv *GoLog) unpackReceiveZapWrapPayload(mesg string, buf []byte, unpack interface{}, level zapcore.Level, wrapNum int, fields []zap.Field) {
	// gv.mutex.Lock()

	// if level >= gv.level {
	e := vclock.VClockPayload{}
	e.Payload = unpack

	// Just use msgpack directly
	err := gv.decodingStrategy(buf, &e)
	if err != nil {
		gv.internalLogger.Printf("for mesg %v, got err %v, num bytes is %v\n", mesg, err.Error(), len(buf))
	}

	// gv.logger.Printf("Merging incoming clocks for %v\n", gv.logfile)
	// Increment and merge the incoming clock
	// gv.mergeIncomingClockZap(mesg, e, level, fields)
	gv.mutex.Lock()
	gv.currentVC.Merge(e.VcMap)
	gv.mutex.Unlock()

	if wrapNum == 1 {
		gv.wrappedLogger.Log(level, mesg, fields...)
	} else {
		gv.wrappedLoggerTwice.Log(level, mesg, fields...)
	}

	// }
	// gv.mutex.Unlock()
}

func (gv *GoLog) UnpackReceiveZap(mesg string, buf []byte, level zapcore.Level, fields ...zap.Field) {
	val := 0
	gv.unpackReceiveZapWrapPayload(mesg, buf, &val, level, 2, fields)
}

// StartBroadcast allows to use vector clocks in the context of casual broadcasts
// sent via RPC. Any call to StartBroadcast must have a corresponding call to
// StopBroadcast, otherwise a deadlock will occur. All RPC calls made in-between
// the calls to StartBroadcast and StopBroadcast will be logged as a single event,
// will be sent out with the same vector clock and will represent broadcast messages
// from the current process to the process pool.
func (gv *GoLog) StartBroadcast(mesg string, opts GoLogOptions) {
	gv.mutex.Lock()
	// gv.tickClock()
	gv.logWriteWrapper(mesg, "Something went wrong, could not log prepare send", opts.Priority)
	gv.broadcast = true
}

// StopBroadcast is called once all RPC calls of a message broadcast have been sent.
func (gv *GoLog) StopBroadcast() {
	gv.broadcast = false
	gv.mutex.Unlock()
}

// TODO
// implement choosing next search index in disvis
// change shiviz->disviz everywhere
// make graph of performance in disviz, using same logs from previous graph
