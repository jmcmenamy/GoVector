package govec

import (
	"runtime/debug"
	"testing"

	"github.com/jmcmenamy/GoVector/govec/vclock"
	//"fmt"
)

var TestPID = "TestPID"

func TestBasicInit(t *testing.T) {

	gv := InitGoVector(TestPID, "TestLogFile", GetDefaultRegexConfig())

	if gv.pid != TestPID {
		t.Fatalf("Setting Process ID Failed.")
	}

	vc := gv.GetCurrentVC()
	n, found := vc.FindTicks(TestPID)

	AssertTrue(t, found, "Initializing clock: Init PID not found")
	AssertEquals(t, uint64(0), n, "Initializing clock: wrong initial clock value")

}

func TestInitialVC(t *testing.T) {
	initialVC := vclock.VClock(map[string]uint64{
		TestPID: 7,
	})

	config := GetDefaultRegexConfig()
	config.InitialVC = initialVC.Copy()
	gv := InitGoVector(TestPID, "TestLogFile", config)

	vc := gv.GetCurrentVC()
	n, found := vc.FindTicks(TestPID)

	AssertTrue(t, found, "Initializing clock: Init PID not found")
	AssertEquals(t, initialVC[TestPID], n, "Initializing clock: wrong initial clock value")
}

func TestLogLocal(t *testing.T) {

	gv := InitGoVector(TestPID, "TestLogFile", GetDefaultRegexConfig())
	opts := GetDefaultLogOptions()
	gv.LogLocalEvent("TestMessage1", opts)

	vc := gv.GetCurrentVC()
	n, _ := vc.FindTicks(TestPID)

	AssertEquals(t, uint64(1), n, "LogLocalEvent: Clock value not incremented")

}

func TestSendAndUnpackInt(t *testing.T) {

	gv := InitGoVector(TestPID, "TestLogFile", GetDefaultRegexConfig())
	opts := GetDefaultLogOptions()
	packed := gv.PrepareSendRegex("TestMessage1", 1337, opts)

	vc := gv.GetCurrentVC()
	n, _ := vc.FindTicks(TestPID)

	AssertEquals(t, uint64(1), n, "PrepareSend: Clock value incremented")

	var response int
	gv.UnpackReceiveRegex("TestMessage2", packed, &response, opts)

	vc = gv.GetCurrentVC()
	n, _ = vc.FindTicks(TestPID)

	AssertEquals(t, 1337, response, "PrepareSend: Clock value incremented.")
	AssertEquals(t, uint64(2), n, "PrepareSend: Clock value incremented.")

}

func TestSendAndUnpackStrings(t *testing.T) {

	gv := InitGoVector(TestPID, "TestLogFile", GetDefaultRegexConfig())
	opts := GetDefaultLogOptions()
	packed := gv.PrepareSendRegex("TestMessage1", "DistClocks!", opts)

	vc := gv.GetCurrentVC()
	n, _ := vc.FindTicks(TestPID)

	AssertEquals(t, uint64(1), n, "PrepareSend: Clock value incremented. ")

	var response string
	gv.UnpackReceiveRegex("TestMessage2", packed, &response, opts)

	vc = gv.GetCurrentVC()
	n, _ = vc.FindTicks(TestPID)

	AssertEquals(t, "DistClocks!", response, "PrepareSend: Clock value incremented.")
	AssertEquals(t, uint64(2), n, "PrepareSend: Clock value incremented.")

}

func TestBroadcast(t *testing.T) {

	gv := InitGoVector(TestPID, "TestLogFile", GetDefaultRegexConfig())
	opts := GetDefaultLogOptions()

	gv.StartBroadcast("TestBroadcast", opts)
	var packed []byte

	for i := 0; i < 5; i++ {
		packed = gv.PrepareSendRegex("", 1337, opts)
	}

	gv.StopBroadcast()

	vc := gv.GetCurrentVC()
	n, _ := vc.FindTicks(TestPID)

	AssertEquals(t, uint64(1), n, "PrepareSend: Clock value incremented")

	var response int
	gv.UnpackReceiveRegex("TestMessage", packed, &response, opts)

	vc = gv.GetCurrentVC()
	n, _ = vc.FindTicks(TestPID)

	AssertEquals(t, 1337, response, "PrepareSend: Clock value incremented.")
	AssertEquals(t, uint64(2), n, "PrepareSend: Clock value incremented.")
}

func BenchmarkPrepare(b *testing.B) {

	gv := InitGoVector(TestPID, "TestLogFile", GetDefaultRegexConfig())
	opts := GetDefaultLogOptions()

	var packed []byte

	for i := 0; i < b.N; i++ {
		packed = gv.PrepareSendRegex("TestMessage1", 1337, opts)
	}

	var response int
	gv.UnpackReceiveRegex("TestMessage2", packed, &response, opts)

}

func BenchmarkUnpack(b *testing.B) {

	gv := InitGoVector(TestPID, "TestLogFile", GetDefaultRegexConfig())
	opts := GetDefaultLogOptions()

	var packed []byte
	packed = gv.PrepareSendRegex("TestMessage1", 1337, opts)

	var response int

	for i := 0; i < b.N; i++ {
		gv.UnpackReceiveRegex("TestMessage2", packed, &response, opts)
	}

}

func AssertTrue(t *testing.T, condition bool, message string) {
	if !condition {
		t.Fatalf(message)
	}
}

func AssertEquals(t *testing.T, expected interface{}, actual interface{}, message string) {
	if expected != actual {
		debug.PrintStack()
		t.Fatalf(message+"Expected: %s, Actual: %s", expected, actual)
	}
}
