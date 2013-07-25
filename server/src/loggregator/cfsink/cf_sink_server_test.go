package cfsink

import (
	"bufio"
	"code.google.com/p/gogoprotobuf/proto"
	"github.com/cloudfoundry/gosteno"
	"github.com/stretchr/testify/assert"
	"io"
	"io/ioutil"
	"logMessage"
	"net/http"
	"strings"
	"testing"
	"time"
)

func WaitForWebsocketRegistration() {
	time.Sleep(50 * time.Millisecond)
}

func AddSink(t *testing.T, receivedChan chan []byte, port string, path string) {
	url := "http://localhost:" + port + path
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Errorf("Could not create request: %s", err)
	}
	rs, err := http.DefaultClient.Do(request)

	if err != nil {
		t.Errorf("Could not create request: %s", err)
	}
	go func() {

		reader := bufio.NewReader(rs.Body)
		for {
			data := make([]byte, 4096)
			n, err := reader.Read(data)
			if err == io.EOF {
				break
			}
			receivedChan <- data[:n]
		}
	}()
	return
}

func AddFailingSink(t *testing.T, port string, path string, status int) string {
	url := "http://localhost:" + port + path
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Errorf("Could not create request: %s", err)
	}
	rs, err := http.DefaultClient.Do(request)

	if err != nil {
		t.Errorf("Could not create request: %s", err)
	}
	assert.Equal(t, status, rs.StatusCode)
	body, err := ioutil.ReadAll(rs.Body)
	assert.NoError(t, err)
	return string(body)
}

func MarshalledLogMessage(t *testing.T, messageString string, spaceId string, appId string) []byte {
	currentTime := time.Now()

	messageType := logMessage.LogMessage_OUT
	sourceType := logMessage.LogMessage_DEA
	protoMessage := &logMessage.LogMessage{
		Message:     []byte(messageString),
		AppId:       proto.String(appId),
		SpaceId:     proto.String(spaceId),
		MessageType: &messageType,
		SourceType:  &sourceType,
		Timestamp:   proto.Int64(currentTime.UnixNano()),
	}
	message, err := proto.Marshal(protoMessage)
	assert.NoError(t, err)

	return message
}

func AssertProtoBufferMessageEquals(t *testing.T, expectedMessage string, actual []byte) {
	receivedMessage := &logMessage.LogMessage{}
	err := proto.Unmarshal(actual, receivedMessage)
	assert.NoError(t, err)
	assert.Equal(t, expectedMessage, string(receivedMessage.GetMessage()))
}

func SuccessfulAuthorizer(a, b, c, d string, l *gosteno.Logger) bool {
	if b != "" {
		authString := strings.Split(b, " ")
		if len(authString) > 1 {
			return authString[1] == "correctAuthorizationToken"
		}
	}
	return false
}

var sinkServer *cfSinkServer
var dataReadChannel chan []byte

func init() {
	dataReadChannel = make(chan []byte, 10)
	sinkServer = NewCfSinkServer(dataReadChannel, logger(), "localhost:8081", "/tail/", "http://localhost:9876", SuccessfulAuthorizer)
	go sinkServer.Start()
	time.Sleep(1 * time.Millisecond)
}

func TestThatItSends(t *testing.T) {
	receivedChan := make(chan []byte, 2)

	expectedMessageString := "Some data"
	expectedMessage := MarshalledLogMessage(t, expectedMessageString, "mySpace", "myApp")
	otherMessageString := "Some more stuff"
	otherMessage := MarshalledLogMessage(t, otherMessageString, "mySpace", "myApp")

	AddSink(t, receivedChan, "8081", "/tail/spaces/mySpace/apps/myApp?authorization=bearer%20correctAuthorizationToken")
	WaitForWebsocketRegistration()

	dataReadChannel <- expectedMessage
	dataReadChannel <- otherMessage

	select {
	case <-time.After(1 * time.Second):
		t.Errorf("Did not get message.")
	case message := <-receivedChan:
		AssertProtoBufferMessageEquals(t, expectedMessageString, message)
	}

	select {
	case <-time.After(1 * time.Second):
		t.Errorf("Did not get message.")
	case message := <-receivedChan:
		AssertProtoBufferMessageEquals(t, otherMessageString, message)
	}
}

func TestThatItSendsAllDataToAllSinks(t *testing.T) {
	client1ReceivedChan := make(chan []byte)
	client2ReceivedChan := make(chan []byte)
	space1ReceivedChan := make(chan []byte)
	space2ReceivedChan := make(chan []byte)

	expectedMessageString := "Some Data"
	expectedMarshalledProtoBuffer := MarshalledLogMessage(t, expectedMessageString, "mySpace", "myApp")

	AddSink(t, client1ReceivedChan, "8081", "/tail/spaces/mySpace/apps/myApp?authorization=bearer%20correctAuthorizationToken")

	AddSink(t, client2ReceivedChan, "8081", "/tail/spaces/mySpace/apps/myApp?authorization=bearer%20correctAuthorizationToken")
	WaitForWebsocketRegistration()

	AddSink(t, space1ReceivedChan, "8081", "/tail/spaces/mySpace?authorization=bearer%20correctAuthorizationToken")
	WaitForWebsocketRegistration()

	AddSink(t, space2ReceivedChan, "8081", "/tail/spaces/mySpace?authorization=bearer%20correctAuthorizationToken")
	WaitForWebsocketRegistration()

	dataReadChannel <- expectedMarshalledProtoBuffer

	AssertProtoBufferMessageEquals(t, expectedMessageString, <-client1ReceivedChan)
	AssertProtoBufferMessageEquals(t, expectedMessageString, <-client2ReceivedChan)
	AssertProtoBufferMessageEquals(t, expectedMessageString, <-space1ReceivedChan)
	AssertProtoBufferMessageEquals(t, expectedMessageString, <-space2ReceivedChan)
}

func TestThatItSendsLogsForOneApplication(t *testing.T) {
	receivedChan := make(chan []byte, 2)

	otherAppsMarshalledMessage := MarshalledLogMessage(t, "Some other message", "mySpace", "otherApp")
	expectedMessageString := "My important message"
	myAppsMarshalledMessage := MarshalledLogMessage(t, expectedMessageString, "mySpace", "myApp")

	AddSink(t, receivedChan, "8081", "/tail/spaces/mySpace/apps/myApp?authorization=bearer%20correctAuthorizationToken")
	WaitForWebsocketRegistration()

	dataReadChannel <- otherAppsMarshalledMessage
	dataReadChannel <- myAppsMarshalledMessage

	AssertProtoBufferMessageEquals(t, expectedMessageString, <-receivedChan)
}

func TestThatItSendsLogsForOneSpace(t *testing.T) {
	receivedChan := make(chan []byte, 2)

	otherAppsMarshalledMessage := MarshalledLogMessage(t, "Some other message", "mySpace", "otherApp")
	expectedMessageString := "My important message"
	myAppsMarshalledMessage := MarshalledLogMessage(t, expectedMessageString, "mySpace", "myApp")

	AddSink(t, receivedChan, "8081", "/tail/spaces/mySpace?authorization=bearer%20correctAuthorizationToken")
	WaitForWebsocketRegistration()

	dataReadChannel <- otherAppsMarshalledMessage

	dataReadChannel <- myAppsMarshalledMessage

	AssertProtoBufferMessageEquals(t, "Some other message", <-receivedChan)

	AssertProtoBufferMessageEquals(t, expectedMessageString, <-receivedChan)
}

func TestDropUnmarshallableMessage(t *testing.T) {
	receivedChan := make(chan []byte)

	AddSink(t, receivedChan, "8081", "/tail/spaces/mySpace/apps/myApp?authorization=bearer%20correctAuthorizationToken")
	WaitForWebsocketRegistration()

	dataReadChannel <- make([]byte, 10)

	time.Sleep(1 * time.Millisecond)
	select {
	case msg1 := <-receivedChan:
		t.Error("We should not have received a message, but got: %v", msg1)
	default:
		//no communication. That's good!
	}
}

func TestDropSinkWithoutApp(t *testing.T) {
	message := AddFailingSink(t, "8081", "/tail/", 400)
	assert.Contains(t, message, "Did not accept sink connection")
}

func TestDropSinkWithoutAuthorization(t *testing.T) {
	message := AddFailingSink(t, "8081", "/tail/spaces/mySpace/apps/myApp", 400)
	assert.Contains(t, message, "Did not accept sink connection")
}

func TestDropSinkWhenAuthorizationFails(t *testing.T) {
	message := AddFailingSink(t, "8081", "/tail/spaces/mySpace/apps/myApp?authorization=incorrectAuthToken", 401)
	assert.Contains(t, message, "User not authorized to access space")
}