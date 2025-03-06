// Package govec is a vector clock logging library
package govec

import (
	"bytes"
	"encoding/json"
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

// LogPriority controls the minimum priority of logging events which
// will be logged.
type LogPriority int

// LogPriority enum provides all the valid Priority Levels that can be
// used to log events with.
const (
	DEBUG LogPriority = iota
	INFO
	WARNING
	ERROR
	FATAL
)

// colorLookup is the array of status to string from runtime/proc.go
var colorLookup = [...]ct.Color{
	DEBUG:   ct.Green,
	INFO:    ct.White,
	WARNING: ct.Yellow,
	ERROR:   ct.Red,
	FATAL:   ct.Magenta,
}

// prefixLookup translates priority enums into strings
var prefixLookup = [...]string{
	DEBUG:   "DEBUG",
	INFO:    "INFO",
	WARNING: "WARNING",
	ERROR:   "ERROR",
	FATAL:   "FATAL",
}

// priorityToLevel translates LogPriority to zapcore.Level
var priorityToLevel = map[LogPriority]zapcore.Level{
	INFO:    zap.InfoLevel,
	WARNING: zap.WarnLevel,
	DEBUG:   zap.DebugLevel,
	ERROR:   zap.ErrorLevel,
	FATAL:   zap.FatalLevel,
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
	Priority LogPriority
	Level    zapcore.Level
	// InitialVC is the initial vector clock value, nil by default
	InitialVC vclock.VClock
	// Whether or not to generate logs with zap, which supports arbitrary key value pairs
	GenerateZapLogs bool
	// Whether or not to generate logs using regex style, compatible with the original Shiviz
	GenerateRegexLogs bool
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
		Priority:          INFO,
		Level:             zapcore.InfoLevel,
		InitialVC:         nil,
		GenerateZapLogs:   false,
		GenerateRegexLogs: true,
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
	Priority LogPriority
}

// GetDefaultLogOptions returns the default GoLogOptions with default values
// for the fields
func GetDefaultLogOptions() GoLogOptions {
	o := GoLogOptions{Priority: INFO}
	return o
}

// SetPriority returns a new GoLogOptions object with its priority field
// set to Priority. Follows the builder pattern.
// Priority : (GoLogPriority) The Priority that the new GoLogOptions object must have
func (o *GoLogOptions) SetPriority(Priority LogPriority) GoLogOptions {
	opts := *o
	opts.Priority = Priority
	return opts
}

// Represents an entry that we log later, once the zap logger is initialized
type ZapEntryInput struct {
	entry  *zapcore.Entry
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
	buffered atomic.Bool

	// Flag to include timestamps when logging events
	usetimestamps bool

	// Flag to indicate if the log file will contain multiple executions
	appendLog bool

	// Flag to indicate if the message broadcast is on
	broadcast bool

	// Priority level at which all events are logged
	priority LogPriority
	level    zapcore.Level

	// Logfile name
	logfile string

	// buffered string
	output                   string
	writeSyncer              zapcore.WriteSyncer
	preInitializationEntries []*ZapEntryInput
	preInitializationLogs    []*ZapLogInput

	// encoding and decoding strategies for network messages
	encodingStrategy func(interface{}) ([]byte, error)
	decodingStrategy func([]byte, interface{}) error

	// Internal logger for printing errors
	logger *log.Logger

	mutex sync.RWMutex

	zapLogger *zap.Logger
}

// Given an existing zap Logger, return a new zap logger that will
// keep the same configuration, but also write any logs to this GoLog logger
func (gv *GoLog) WrapBaseZapLogger(baseLogger *zap.Logger) *zap.Logger {
	return baseLogger.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		// could use zapcore.NewTee here but still would need to embed this core
		return &GoLogZapCore{
			Core: core,
			gv:   gv,
		}
	}), zap.AddCaller(), zap.AddStacktrace(min(baseLogger.Level(), gv.level)))
}

// prepareGoLogWriteSyncer opens the filePaths in append mode (or creates it if it doesn't exist),
// and sets the writeSyncer of gv to a GoLogWriteSyncer safe for concurrent use.
func (gv *GoLog) prepareGoLogWriteSyncer(filePaths []string) error {
	baseWriteSyncer, _, err := zap.Open(filePaths...)
	if err != nil {
		return err
	}
	gv.writeSyncer = zapcore.Lock(&GoLogWriteSyncer{
		buffer:          new(bytes.Buffer),
		baseWriteSyncer: baseWriteSyncer,
		gv:              gv,
	})
	return nil
}

// Creates an Uninitialized GoLog that can still be logged to with zap logs
// Loggs are buffered and will be logged immediately once this logger is initialized
// and output paths are given
func UninitializedGoVector() *GoLog {
	goLog := &GoLog{logging: true, loggingZap: true}
	if logToTerminal {
		goLog.logger = log.New(os.Stdout, "[GoVector]:", log.Lshortfile)
	} else {
		goLog.logger = log.New(io.Discard, "[GoVector]:", log.Lshortfile)
	}
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

// InitGoVector returns a GoLog which generates a logs prefixed with
// processid, to files with names logfilenames, appended with a -Log.txt or -zap-Log.txt. Any old log with the same
// name will be truncated for non-zap logs. zap logs will be parsed and used to initialize the vector clock
// Config controls logging options. See GoLogConfig for more details.
func (gv *GoLog) InitGoVector(processid string, config GoLogConfig, logfilenames ...string) {
	gv.mutex.Lock()
	defer gv.mutex.Unlock()
	// gv := &GoLog{}
	gv.pid = processid

	if logToTerminal && gv.logger == nil {
		gv.logger = log.New(os.Stdout, "[GoVector]:", log.Lshortfile)
	} else {
		gv.logger = log.New(io.Discard, "[GoVector]:", log.Lshortfile)
	}

	//Set parameters from config
	gv.printonscreen = config.PrintOnScreen
	gv.usetimestamps = config.UseTimestamps
	gv.priority = config.Priority
	gv.level = config.Level
	if priorityToLevel[gv.priority] != gv.level {
		if gv.level == zapcore.DPanicLevel || gv.level == zapcore.PanicLevel {
			gv.priority = INFO
		} else {
			gv.level = priorityToLevel[gv.priority]
		}
	}
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
		gv.logger.Printf("No file names passed in")
	}
	gv.logfile = logfilenames[0] + "-Log.txt"
	if gv.loggingZap {
		for i, logfilename := range logfilenames {
			logfilenames[i] = logfilename + "-zap-Log.txt"
		}
		err := gv.prepareZapLogger(logfilenames)
		if err != nil {
			gv.logger.Printf("Got err creating zap loger: %v\n", err)
		}
		if len(gv.preInitializationLogs) > 0 {
			gv.logLocalEventZapUnlocked(zapcore.InfoLevel, "Going to log the backed up messages", make([]zap.Field, 0))
		}
		for _, zapLog := range gv.preInitializationLogs {
			gv.logLocalEventZapUnlocked(zapLog.level, zapLog.msg, zapLog.fields)
		}
		if len(gv.preInitializationEntries) > 0 {
			gv.logLocalEventZapUnlocked(zapcore.InfoLevel, "Going to log the backed up entries", make([]zap.Field, 0))
		}
		for _, zapLog := range gv.preInitializationEntries {
			gv.logLocalEntryZapUnlocked(zapLog.entry, zapLog.fields)
		}
		gv.preInitializationLogs = nil
		gv.preInitializationEntries = nil
		gv.Sync()
		gv.buffered.Store(config.Buffered)
	}
	if gv.loggingRegex {
		gv.prepareLogFile()
		gv.Flush()
	}
}

// Assumes gv.logfilename has been set
// prepares gv.logfilename to be written to with regex style logs
func (gv *GoLog) prepareLogFile() {
	_, err := os.Stat(gv.logfile)
	if err == nil {
		if !gv.appendLog {
			gv.logger.Println(gv.logfile, "exists! ... Deleting ")
			os.Remove(gv.logfile)
		} else {
			executionnumber := time.Now().Format(time.UnixDate)
			gv.logger.Println("Execution Number is  ", executionnumber)
			executionstring := "=== Execution #" + executionnumber + "  ==="
			gv.logThis(executionstring, "", "", gv.priority)
			return
		}
	}
	// Create directory path to log if it doesn't exist.
	if err := os.MkdirAll(filepath.Dir(gv.logfile), 0750); err != nil {
		gv.logger.Println(err)
	}

	//Creating new Log
	file, err := os.Create(gv.logfile)
	if err != nil {
		gv.logger.Println(err)
	}

	file.Close()

	if gv.appendLog {
		executionnumber := time.Now().Format(time.UnixDate)
		gv.logger.Println("Execution Number is  ", executionnumber)
		executionstring := "=== Execution #" + executionnumber + "  ==="
		gv.logThis(executionstring, "", "", gv.priority)
	}

	gv.currentVC.Tick(gv.pid)
	ok := gv.logThis("Initialization Complete", gv.pid, gv.currentVC.ReturnVCString(), gv.priority)
	if !ok {
		gv.logger.Println("Something went Wrong, Could not Log!")
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
func (gv *GoLog) prepareZapLogger(filePaths []string) error {
	var firstFilePath string
	for _, filePath := range filePaths {
		if _, err := os.Stat(filePath); err == nil {
			firstFilePath = filePath
			break
		}
	}
	// If the log file already exists, read and process its last line.
	if firstFilePath != "" {
		lastLine, err := readLastLine(firstFilePath)
		if err != nil {
			return fmt.Errorf("failed to read last line: %w", err)
		}
		if lastLine != "" {

			var jsonObj map[string]interface{}
			if err := json.Unmarshal([]byte(lastLine), &jsonObj); err != nil {
				gv.logger.Printf("Error parsing JSON: %v\n", err)
			} else {
				if vc, ok := jsonObj["VCString"]; ok {
					// gv.logger.Printf("VCString: %v\n", vc)
					// gv.logger.Printf("VCString type: %v\n", reflect.TypeOf(vc))
					VCMap := make(map[string]uint64)
					for k, v := range vc.(map[string]interface{}) {
						VCMap[k] = uint64(v.(float64))
					}
					gv.logger.Printf("Merged clock was %v\n", gv.currentVC.ReturnVCString())
					gv.currentVC.Merge(VCMap)
					gv.logger.Printf("Merged clock is now %v\n", gv.currentVC.ReturnVCString())
				} else {
					gv.logger.Printf("VCString field not found in JSON\n")
				}
			}
		}
	}

	// Open the log file (zap handles file creation)
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
		gv.writeSyncer,
		gv.level, // Log level
	)

	// Create the logger
	gv.zapLogger = zap.New(core, zap.AddCaller(), zap.AddStacktrace(gv.level))

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
func (gv *GoLog) EnableBufferedWrites() {
	gv.buffered.Store(true)
}

// DisableBufferedWrites disables buffered writes to the log file. All
// the log messages from now on will be written to the Log file
// immediately. Writes all the existing log messages that haven't been
// written to Log file yet.
func (gv *GoLog) DisableBufferedWrites() {
	gv.buffered.Store(false)
	// gv.buffered = false
	if gv.output != "" {
		gv.Flush()
	}
	// sync any logs buffered in our GoLogWriteSyncer
	gv.zapLogger.Sync()
}

// Flush writes the log messages stored in the buffer to the Log File.
// This function should be used by the application to also force writes
// in the case of interrupts and crashes.   Note: Calling Flush when
// BufferedWrites is disabled is essentially a no-op.
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

func (gv *GoLog) printColoredMessage(LogMessage string, Priority LogPriority) {
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
func (gv *GoLog) logThis(Message string, ProcessID string, VCString string, Priority LogPriority) bool {
	var (
		complete = true
		buffer   bytes.Buffer
	)

	if gv.loggingRegex {
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
		gv.zapLogger.Log(gv.level, Message,
			zap.String("processId", gv.pid),
			gv.currentVC.ReturnVCStringZap("VCString"))
	}
	return complete
}

// Log logs a message at the specified level. The message includes any fields
// passed at the log site, as well as any fields accumulated on the logger.
// Any Fields that require  evaluation (such as Objects) are evaluated upon
// invocation of Log.
func (gv *GoLog) Log(lvl zapcore.Level, msg string, fields ...zap.Field) {
	gv.logLocalEventZap(lvl, msg, fields)
}

// Debug logs a message at DebugLevel. The message includes any fields passed
// at the log site, as well as any fields accumulated on the logger.
func (gv *GoLog) Debug(msg string, fields ...zap.Field) {
	gv.logLocalEventZap(zapcore.DebugLevel, msg, fields)
}

// Info logs a message at InfoLevel. The message includes any fields passed
// at the log site, as well as any fields accumulated on the logger.
func (gv *GoLog) Info(msg string, fields ...zap.Field) {
	gv.logLocalEventZap(zapcore.InfoLevel, msg, fields)
}

// Warn logs a message at WarnLevel. The message includes any fields passed
// at the log site, as well as any fields accumulated on the logger.
func (gv *GoLog) Warn(msg string, fields ...zap.Field) {
	gv.logLocalEventZap(zapcore.WarnLevel, msg, fields)
}

// Error logs a message at ErrorLevel. The message includes any fields passed
// at the log site, as well as any fields accumulated on the logger.
func (gv *GoLog) Error(msg string, fields ...zap.Field) {
	gv.logLocalEventZap(zapcore.ErrorLevel, msg, fields)
}

// DPanic logs a message at DPanicLevel. The message includes any fields
// passed at the log site, as well as any fields accumulated on the logger.
//
// If the logger is in development mode, it then panics (DPanic means
// "development panic"). This is useful for catching errors that are
// recoverable, but shouldn't ever happen.
func (gv *GoLog) DPanic(msg string, fields ...zap.Field) {
	gv.logLocalEventZap(zapcore.DPanicLevel, msg, fields)
}

// Panic logs a message at PanicLevel. The message includes any fields passed
// at the log site, as well as any fields accumulated on the logger.
//
// The logger then panics, even if logging at PanicLevel is disabled.
func (gv *GoLog) Panic(msg string, fields ...zap.Field) {
	gv.logLocalEventZap(zapcore.PanicLevel, msg, fields)
}

// Fatal logs a message at FatalLevel. The message includes any fields passed
// at the log site, as well as any fields accumulated on the logger.
//
// The logger then calls os.Exit(1), even if logging at FatalLevel is
// disabled.
func (gv *GoLog) Fatal(msg string, fields ...zap.Field) {
	gv.logLocalEventZap(zapcore.FatalLevel, msg, fields)
}

// Sync calls the underlying Core's Sync method, flushing any buffered log
// entries. Applications should take care to call Sync before exiting.
func (gv *GoLog) Sync() {
	if gv.loggingZap {
		gv.zapLogger.Sync()
	}
}

// logWriteWrapper is a helper function for wrapping common logging
// behaviour associated with logThis
func (gv *GoLog) logWriteWrapper(mesg, errMesg string, priority LogPriority) (success bool) {
	// gv.logger.Printf("HEY LOOK logging this: %v\n", mesg)
	if gv.logging {
		prefix := prefixLookup[priority]
		wrappedMesg := prefix + " " + mesg
		success = gv.logThis(wrappedMesg, gv.pid, gv.currentVC.ReturnVCString(), priority)
		if !success {
			// gv.logger.Printf("HEY LOOK logging this cause of error: %v\n", errMesg)
			gv.logger.Println(errMesg)
		}
	}
	return
}

// Increment GoVectors local clock by 1
func (gv *GoLog) tickClock() {
	_, found := gv.currentVC.FindTicks(gv.pid)
	if !found {
		gv.logger.Println("Couldn't find this process's id in its own vector clock!")
	}
	gv.currentVC.Tick(gv.pid)
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
	if opts.Priority >= gv.priority {
		gv.tickClock()
		logSuccess = gv.logWriteWrapper(mesg, "Something went Wrong, Could not Log LocalEvent!", opts.Priority)
	}
	gv.mutex.Unlock()
	return
}

// Adds processId and VCString fields to a list of zap fields
func (gv *GoLog) addFieldsToLog(fields []zap.Field) []zap.Field {
	return append(fields,
		zap.String("processId", gv.pid),
		gv.currentVC.ReturnVCStringZap("VCString"),
	)
}

// Assumes we are holding gv.mutex
// Ticks the clock and logs the given msg and fields and level level
// If the zap logger isn't initialized yet, just store it for logging later
// Returns whether we successfully passed the log to the logger. If false, the log should be stored for later when the log is initialized
// Assumes we are holding gv.mutex and are are logging and have a defined logger
func (gv *GoLog) logLocalEventZapUnlocked(level zapcore.Level, msg string, fields []zap.Field) {
	if ce := gv.zapLogger.Check(level, msg); ce != nil {
		gv.tickClock()
		ce.Write(gv.addFieldsToLog(fields)...)
	}
}

// Same as logLocalEventZapUnlocked but the inputs are different, mainly for use in GoLogZapCore.Write()
func (gv *GoLog) logLocalEntryZapUnlocked(entry *zapcore.Entry, fields []zap.Field) {
	gv.tickClock()
	gv.zapLogger.Core().Write(*entry, gv.addFieldsToLog(fields))
}

// Same as logLocalEntryZapUnlocked but we grab the lock first and check if we are logging at all
func (gv *GoLog) logLocalEventZap(level zapcore.Level, msg string, fields []zap.Field) {
	if !gv.loggingZap {
		return
	}
	gv.mutex.Lock()
	defer gv.mutex.Unlock()
	if gv.zapLogger == nil {
		gv.preInitializationLogs = append(gv.preInitializationLogs, &ZapLogInput{level: level, msg: msg, fields: fields})
		return
	}
	gv.logLocalEventZapUnlocked(level, msg, fields)
}

// Same as logLocalEventZap but the inputs are different, mainly for use in GoLogZapCore.Write()
func (gv *GoLog) logLocalEntryZap(entry zapcore.Entry, fields []zap.Field) {
	if !gv.loggingZap {
		return
	}
	gv.mutex.Lock()
	defer gv.mutex.Unlock()
	if gv.zapLogger == nil {
		gv.preInitializationEntries = append(gv.preInitializationEntries, &ZapEntryInput{entry: &entry, fields: fields})
		return
	}
	gv.logLocalEntryZapUnlocked(&entry, fields)
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
		if opts.Priority >= gv.priority {
			gv.tickClock()

			// gv.logger.Printf("In prepare send writing to %v\n", gv.logfile)
			gv.logWriteWrapper(mesg, "Something went wrong, could not log prepare send", opts.Priority)

			d := vclock.VClockPayload{Pid: gv.pid, VcMap: gv.currentVC.GetMap(), Payload: buf}

			// encode the Clock Payload
			var err error
			encodedBytes, err = gv.encodingStrategy(&d)
			if err != nil {
				gv.logger.Println(err.Error())
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
			gv.logger.Println(err.Error())
		}
	}
	return
}

func (gv *GoLog) mergeIncomingClock(mesg string, e vclock.VClockPayload, Priority LogPriority) {
	// First, tick the local clock
	gv.tickClock()
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

	if opts.Priority >= gv.priority {
		e := vclock.VClockPayload{}
		e.Payload = unpack

		// Just use msgpack directly
		err := gv.decodingStrategy(buf, &e)
		if err != nil {
			gv.logger.Println(err.Error())
		}

		// gv.logger.Printf("Merging incoming clocks for %v\n", gv.logfile)
		// Increment and merge the incoming clock
		gv.mergeIncomingClock(mesg, e, opts.Priority)
	}
	gv.mutex.Unlock()

}

// StartBroadcast allows to use vector clocks in the context of casual broadcasts
// sent via RPC. Any call to StartBroadcast must have a corresponding call to
// StopBroadcast, otherwise a deadlock will occur. All RPC calls made in-between
// the calls to StartBroadcast and StopBroadcast will be logged as a single event,
// will be sent out with the same vector clock and will represent broadcast messages
// from the current process to the process pool.
func (gv *GoLog) StartBroadcast(mesg string, opts GoLogOptions) {
	gv.mutex.Lock()
	gv.tickClock()
	gv.logWriteWrapper(mesg, "Something went wrong, could not log prepare send", opts.Priority)
	gv.broadcast = true
}

// StopBroadcast is called once all RPC calls of a message broadcast have been sent.
func (gv *GoLog) StopBroadcast() {
	gv.broadcast = false
	gv.mutex.Unlock()
}
