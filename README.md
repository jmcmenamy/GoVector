<picture>
  <source media="(prefers-color-scheme: dark)" srcset=".images/GoVectorDark.svg">
  <source media="(prefers-color-scheme: light)" srcset=".images/GoVectorLight.svg">
  <img alt="GoVector Logo" src=".images/GoVectorLight.svg">
</picture>

[![GoDoc](https://pkg.go.dev/badge/github.com/jmcmenamy/GoVector)](https://pkg.go.dev/github.com/jmcmenamy/GoVector) [![Go Report Card](https://goreportcard.com/badge/github.com/jmcmenamy/GoVector)](https://goreportcard.com/report/github.com/jmcmenamy/GoVector) [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

----

GoVector is a vector clock logging library written in Go, forked from and built off the original [GoVector](https://github.com/DistributedClocks/GoVector). The [vector clock algorithm](https://en.wikipedia.org/wiki/Vector_clock) is used to order events in distributed systems in the absence of a centralized clock. GoVector implements the vector clock algorithm and provides feature-rich logging using [Zap](https://pkg.go.dev/go.uber.org/zap#section-readme).

Sending vector clocks between nodes in a distributed system
is done using 2 key functions, `PrepareSendZap` and `UnpackReceiveZap`. `PrepareSendZap` updates GoVector's local time, logs a sending event, and returns a byte array to send on the network. `UnpackReceiveZap` decodes messages from the network, merges GoVector's local
clock with the received clock, and logs a receiving event.

This library can be added to a Go project to generate a
[DisViz](https://jmcmenamy.github.io/disviz/)-compatible vector-clock
timestamped log of events in a distributed system.

* `govec/`    	    : Contains the Library and all its dependencies
* `govec/vclock`	: Pure vector clock library
* `govec/vrpc`	    : Go's rpc with GoVector integration
* `example/`  	    : Contains some examples instrumented with different features of GoVector

### Installation

To install GoVector you must have a correctly configured go development
environment. See [How to Write Go
Code](https://go.dev/doc/code).

Once you set up your environment, GoVector can be installed with the go
tool command:

```shell
$ go install github.com/jmcmenamy/GoVector
```

### Usage

The following is a basic example of how this library can be used. The `GoLog` struct is the main object from this package that users will interact with. `GoLog.Info`, `GoLog.PrepareSendZap`, and `GoLog.UnpackReceiveZap` are the main methods users will use from this package. Those last two should be called immediately before and after receiving a message on the network.

```go
package main

import (
	"github.com/jmcmenamy/GoVector/govec"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	// Initialize logger with default zap configuration.
	Logger := govec.InitGoVector("MyProcess", "LogFile", govec.GetDefaultZapConfig())

	// Prior to sending a message, call PrepareSendZapWrapPayload on the payload
	messagepayload := []byte("samplepayload")
	encodedVCpayload := Logger.PrepareSendZapWrapPayload("Sending Message", messagepayload, zapcore.InfoLevel, zap.Int("messageNum", 1))

	// encodedVCpayload is ready to be written to the network
	// Call UnpackReceiveZapWrapPayload on received messages to update local vector clock
	var incommingMessage []byte
	Logger.UnpackReceiveZapWrapPayload("Received Message from server", encodedVCpayload, &incommingMessage, zapcore.InfoLevel)

	// The Zap API is embedded in the GoLog object, so all Zap methods can be called
	Logger.Info("Example Complete", zap.Bool("boolField", false))

	// Instead of wrapping the user payload inside the GoLog payload,
	// PrepareSendZap/UnpackReceiveZap can be used to put the GoLog payload inside the user payload
	type samplePayload struct {
		encodedVCPayload []byte
	}

	payloadToSend := samplePayload{
		encodedVCPayload: Logger.PrepareSendZap("Sending Message", zapcore.InfoLevel, zap.String("stringField", "value")),
	}

	// payload is encoded, sent, then decoded
	// Then, we can grab the GoLog payload out of the decoded user payload.
	Logger.UnpackReceiveZap("Received Message from server", payloadToSend.encodedVCPayload, zapcore.InfoLevel)
}
```

Which produces these logs in `LogFile-zap-Log.txt`:

```
{"level":"INFO","caller":"GoVector/govec.go:79","function":"main.main","message":"Sending Message","messageNum":1,"processId":"MyProcess","VCString":{"MyProcess":1},"stacktrace":"main.main\n\t/Users/josiahmcmenamy/transferred_files/meng_project/GoVector/govec.go:79\nruntime.main\n\t/usr/local/Cellar/go/1.23.5/libexec/src/runtime/proc.go:272"}
{"level":"INFO","caller":"GoVector/govec.go:84","function":"main.main","message":"Received Message from server","processId":"MyProcess","VCString":{"MyProcess":2},"stacktrace":"main.main\n\t/Users/josiahmcmenamy/transferred_files/meng_project/GoVector/govec.go:84\nruntime.main\n\t/usr/local/Cellar/go/1.23.5/libexec/src/runtime/proc.go:272"}
{"level":"INFO","caller":"GoVector/govec.go:87","function":"main.main","message":"Example Complete","incommingMessage":"samplepayload","processId":"MyProcess","VCString":{"MyProcess":3},"stacktrace":"main.main\n\t/Users/josiahmcmenamy/transferred_files/meng_project/GoVector/govec.go:87\nruntime.main\n\t/usr/local/Cellar/go/1.23.5/libexec/src/runtime/proc.go:272"}
{"level":"INFO","caller":"GoVector/govec.go:96","function":"main.main","message":"Sending Message","stringField":"value","processId":"MyProcess","VCString":{"MyProcess":4},"stacktrace":"main.main\n\t/Users/josiahmcmenamy/transferred_files/meng_project/GoVector/govec.go:96\nruntime.main\n\t/usr/local/Cellar/go/1.23.5/libexec/src/runtime/proc.go:272"}
{"level":"INFO","caller":"GoVector/govec.go:101","function":"main.main","message":"Received Message from server","processId":"MyProcess","VCString":{"MyProcess":5},"stacktrace":"main.main\n\t/Users/josiahmcmenamy/transferred_files/meng_project/GoVector/govec.go:101\nruntime.main\n\t/usr/local/Cellar/go/1.23.5/libexec/src/runtime/proc.go:272"}

```

`PrepareSendZap` returns a byte array that should be put in the user's payload that is sent across the network, extracted from the payload when a message is received, and passed to `UnpackReceiveZap`. It is also possible to wrap the user's payload inside the byte array returned from `PrepareSendZapWrapPayload`, so the returned byte array is sent across the network, and the user's payload is decoded using `UnpackReceiveZapWrapPayload`.

For complete documentation with examples see GoVector's [documentation](https://pkg.go.dev/github.com/jmcmenamy/GoVector/govec).

**Note for 6.5840 students**: For the vector clocks in the logs to be valid, it is important that only one process on the device thinks it is a particular raft node at a given time. For example, when I took the class, goroutines acting as a Raft node would learn they've been killed by periodically checking a flag in the struct:

```go
func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}
```

The testing code in `config.go` would crash and restart a raft node by doing `atomic.StoreInt32(&rf.dead, 1)` and making a new `Raft` struct with the same config, which means the new goroutine would be writing to the same file. The old goroutine could be running at the same time for a short while until the next time it checks if it has been killed.

Two goroutines writing to the same file would produce invalid vector clocks, so a band-aid fix was to do:

```go
if !rf.killed() {
    rf.Info(message)
}
```

in my util function that handles logging. It is still possible to produce invalid logs doing this, but I never hit that race condition when testing, so more synchronization wasn't needed.

### Generating DisViz compatible logs

By default, when you download the GoVector package using the go get command, the command installs a binary of the top-level file `govec.go` by the name of GoVector in the directory `$GOPATH/bin`. As long as this directory is part of your path, you can run the GoVector binary to generate a DisViz compatible log from all the logs in a given directory.

**Note**: Make sure that you are running the GoVector binary on a directory that contains log files from the same execution of the system. If it contains logs from multiple executions, then DisViz won't be able to interpret the log file.

#### Usage

To generate a DisViz-compatible log file called `hello.log` from all log files in the directory `path/to/logs` do the following:

```shell
$ GoVector --log_type disviz --log_dir path/to/logs --outfile hello.log
```

See [this repo](https://jmcmenamy.github.io/meng_project/) for a description of how to use these tools together.

### Zap API

The [`GoLog`](https://pkg.go.dev/github.com/jmcmenamy/GoVector/govec#GoLog) struct embeds a Zap [`Logger`](https://pkg.go.dev/go.uber.org/zap#Logger), so the entire Zap API is available from a `GoLog` object (e.g. `GoLog.Info()`, `GoLog.With()`). For a quick intro to Zap, see section 4.2 (page 38) [here](https://github.com/jmcmenamy/meng_project/blob/main/thesis/Josiah_MEng_Thesis.pdf), or the [documentation](https://pkg.go.dev/go.uber.org/zap)

### Motivation

The reason for making a fork of the original [GoVector](https://github.com/DistributedClocks/GoVector) was to make it easier to add information to individual logs. Using the Zap API provides a typesafe way to add fields to each log, and makes it easy to output JSON logs that [DisViz](https://jmcmenamy.github.io/disviz/) can parse and display to the user.

### Modifying GoVector

I have not sufficiently tested this package beyond the capabilities required for my [MEng project](https://jmcmenamy.github.io/meng_project/). There are many opinionated pieces of the implementation, and I'm sure there are bugs lurking around.

If you find a bug or want a feature that doesn't exist, feel free to create an [issue](https://github.com/jmcmenamy/GoVector/issues) or make a [pull request](https://github.com/jmcmenamy/GoVector/pulls)!

If you need to make a change just for your development, it's also easy to use a local copy of this repo. Simply clone this repo, and in places where you need to use it, modify your `go.mod` file to include a [replace directive](https://go.dev/ref/mod#go-mod-file-replace):

```go
replace github.com/jmcmenamy/GoVector => /path/to/your/local/GoVector
```

This tells Go to use your local copy instead of downloading the version from the remote source.

If you use GoVector in academic work, you can cite the following:

```bibtex
@misc{mcmenamy2025disviz,
  author = {Josiah McMenamy},
  title = {DisViz: Visualizing real-world distributed system logs with space time diagrams},
  year = {2025},
  howpublished = {\url{https://github.com/jmcmenamy/meng_project/blob/main/thesis/Josiah_MEng_Thesis.pdf}}
}
```

Happy logging!

<!-- July 2017: Brokers are no longer supported, maybe they will come back.

### VectorBroker

type VectorBroker
   * func Init(logfilename string, pubport string, subport string)

### Usage

    A simple stand-alone program can be found in server/broker/runbroker.go 
    which will setup a broker with command line parameters.
   	Usage is: 
    "go run ./runbroker (-logpath logpath) -pubport pubport -subport subport"

    Tests can be run via GoVector/test/broker_test.go and "go test" with the 
    Go-Check package (https://labix.org/gocheck). To get this package use 
    "go get gopkg.in/check.v1".
    
Detailed Setup:

Step 1:

    Create a Global Variable of type brokervec.VectorBroker and Initialize 
    it like this =

    broker.Init(logpath, pubport, subport)
    
    Where:
    - the logpath is the path and name of the log file you want created, or 
    "" if no log file is wanted. E.g. "C:/temp/test" will result in the file 
    "C:/temp/test-log.txt" being created.
    - the pubport is the port you want to be open for publishers to send
    messages to the broker.
    - the subport is the port you want to be open for subscribers to receive 
    messages from the broker.

Step 2:

    Setup your GoVec so that the real-time boolean is set to true and the correct
    brokeraddr and brokerpubport values are set in the Initialize method you
    intend to use.

Step 3 (optional):

    Setup a Subscriber to connect to the broker via a WebSocket over the correct
    subport. For example, setup a web browser running JavaScript to connect and
    display messages as they are received. Make RPC calls by sending a JSON 
    object of the form:
            var msg = {
            method: "SubManager.AddFilter", 
            params: [{"Nonce":nonce, "Regex":regex}], 
            id: 0
            }
            var text = JSON.stringify(msg)

####   RPC Calls

    Publisher RPC calls are made automatically from the GoVec library if the 
    broker is enabled.
    
    Subscriber RPC calls:
    * AddNetworkFilter(nonce string, reply *string)
        Filters messages so that only network messages are sent to the 
        subscriber.      
    * RemoveNetworkFilter(nonce string, reply *string)
        Filters messages so that both network and local messages are sent to the 
        subscriber.
    * SendOldMessages(nonce string, reply *string)
        Sends any messages received before the requesting subscriber subscribed.
  -->