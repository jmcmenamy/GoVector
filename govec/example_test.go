package govec_test

import (
	"fmt"

	"github.com/jmcmenamy/GoVector/govec"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// GoVector logs can produce JSON logs for visualization with DisViz
func ExampleGoLog_disVizCompatible() {
	//Initialize logger with default zap configuration. This can be done in
	//a var block to ensure that GoVector is initialized at boot time.
	Logger := govec.InitGoVector("MyProcess", "LogFile", govec.GetDefaultZapConfig())

	//An example message
	messagepayload := []byte("samplepayload")

	//Prior to sending a message, call PrepareSendZapWrapPayload on the payload to
	//encode the payload and append this processes vector clock to the
	//payload. This will wrap messagepayload inside encodedVCpayload
	encodedVCpayload := Logger.PrepareSendZapWrapPayload("Sending Message", messagepayload, zapcore.InfoLevel, zap.Int("messageNum", 1))

	//encodedVCpayload is ready to be written to the network
	//ex) conn.Write(encodedVCpayload)

	//Receiving Example
	//First allocate a buffer to receive a message into. This must be
	//the same type as the encoded message. Here incommingMessage and
	//messagepayload are the same type []byte.
	var incommingMessage []byte

	//Prior to unpacking call a message must be received
	//ex) conn.Read(encodedVCPayload)

	//Call UnpackReceiveZapWrapPayload on received messages to update local vector
	//clock values, and decode the original message.
	Logger.UnpackReceiveZapWrapPayload("Received Message from server", encodedVCpayload, &incommingMessage, zapcore.InfoLevel)
	fmt.Printf("Received Message: %s\n", incommingMessage)

	//Important local events can be timestamped with vector clocks
	//using LogLocalEvent, which also increments the local clock.
	Logger.LogLocalEventZap(zapcore.InfoLevel, "Example Complete")

	//The Zap API is also embedded in the GoLog object, so all Zap methods can also be called
	Logger.Info("Example Complete", zap.Bool("boolField", false))

	//Instead of wrapping the user payload inside the GoLog payload,
	//PrepareSendZap/UnpackReceiveZap can be used to put the GoLog payload inside the user payload
	//that is sent over the network.
	type samplePayload struct {
		encodedVCPayload []byte
	}

	payloadToSend := samplePayload{
		encodedVCPayload: Logger.PrepareSendZap("Sending Message", zapcore.InfoLevel, zap.String("stringField", "value")),
	}

	//payloadToSend is ready to be encoded, written to the network, then decoded by another device.

	//Then, we can grab the GoLog payload out of the decoded user payload.
	Logger.UnpackReceiveZap("Received Message from server", payloadToSend.encodedVCPayload, zapcore.InfoLevel)

	// Output:
	// Received Message: samplepayload
}

// Basic example of GoVectors key functions
func ExampleGoLog_basic() {
	//Initialize logger with default configuration. This can be done in
	//a var block to ensure that GoVector is initialized at boot time.
	Logger := govec.InitGoVector("MyProcess", "LogFile", govec.GetDefaultRegexConfig())
	opts := govec.GetDefaultLogOptions()

	//An example message
	messagepayload := []byte("samplepayload")

	//Prior to sending a message, call PrepareSend on the payload to
	//encode the payload and append this processes vector clock to the
	//payload
	encodedVCpayload := Logger.PrepareSend("Sending Message", messagepayload, opts)

	//encodedVCpayload is ready to be written to the network
	//ex) conn.Write(encodedVCpayload)

	//Receiving Example
	//First allocate a buffer to receive a message into. This must be
	//the same type as the encoded message. Here incommingMessage and
	//messagepayload are the same type []byte.
	var incommingMessage []byte

	//Prior to unpacking call a message must be received
	//ex) conn.Read(encodedVCPayload)

	//Call UnpackReceive on received messages to update local vector
	//clock values, and decode the original message.
	Logger.UnpackReceive("Received Message from server", encodedVCpayload, &incommingMessage, opts)
	fmt.Printf("Received Message: %s\n", incommingMessage)

	//Important local events can be timestamped with vector clocks
	//using LogLocalEvent, which also increments the local clock.
	Logger.LogLocalEvent("Example Complete", opts)

	// Output: Received Message: samplepayload
}

// Logging with priority trims all events which are lower from the
// specified priority from the log. This functionality is useful for
// isolating behaviour such as recovery protocols, from common
// behaviour like heartbeats.
func ExampleGoLog_priority() {
	//Access GoVectors default configureation, and set priority
	config := govec.GetDefaultRegexConfig()
	config.Level = zapcore.DebugLevel
	config.PrintOnScreen = true
	//Initialize GoVector
	Logger := govec.InitGoVector("MyProcess", "PrioritisedLogFile", config)
	opts := govec.GetDefaultLogOptions()

	Logger.LogLocalEvent("Debug Priority Event", opts.SetPriority(zapcore.DebugLevel))
	Logger.LogLocalEvent("Info Priority Event", opts.SetPriority(zapcore.InfoLevel))
	Logger.LogLocalEvent("Warning Priority Event", opts.SetPriority(zapcore.WarnLevel))
	Logger.LogLocalEvent("Error Priority Event", opts.SetPriority(zapcore.ErrorLevel))
	Logger.LogLocalEvent("Fatal Priority Event", opts.SetPriority(zapcore.FatalLevel))

	//BUG Output contains timestamps so it cant be tested with *******
	//comments
	//Debug Priority Event
	//Info Priority Event
	//Warning Priority Event
	//Error Priority Event
	//Fatal Priority Event
}

// GoVector logs can be used to associate real time events for
// visualization with TSViz
func ExampleGoLog_tSVizCompatable() {
	//Access config and set timestamps (realtime) to true
	config := govec.GetDefaultRegexConfig()
	config.UseTimestamps = true
	//Initalize GoVector
	Logger := govec.InitGoVector("MyProcess", "LogFile", config)
	opts := govec.GetDefaultLogOptions()

	//In Sending Process

	//Prepare a Message
	messagepayload := []byte("samplepayload")
	finalsend := Logger.PrepareSend("Sending Message", messagepayload, opts)
	//In Receiving Process

	//receive message
	var incommingMessage []byte
	Logger.UnpackReceive("Received Message from server", finalsend, &incommingMessage, opts)
	fmt.Printf("Received Message: %s\n", incommingMessage)
	//Can be called at any point
	Logger.LogLocalEvent("Example Complete", opts)

	// Output: Received Message: samplepayload
}
