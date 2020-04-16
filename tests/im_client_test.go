package tests

import (
	"encoding/json"
	"github.com/GoBelieveIO/im_service/core"
	"github.com/satori/go.uuid"
	"net"
	"testing"
	"time"
)

func TestClient(t *testing.T) {
	if true {
		return
	}
	conn, e := net.Dial("tcp", "127.0.0.1:23000")
	if e != nil {
		t.Fatal(e)
	}

	var body = core.AuthenticationToken{
		Token:      "PGlmvljmmKj7thrNWPu6zWRGKXPWo6",
		PlatformId: 2,
		DeviceId:   "87c6f75ef5d3a93d",
	}
	var msg = core.Message{
		Cmd:     15,
		Seq:     0,
		Version: 1,
		Flag:    0,
		Body:    core.IMessage(&body),
		Meta:    nil,
	}

	e = core.SendMessage(conn, &msg)
	if e != nil {
		t.Fatal(e)
	}

	var message *core.Message
	for {
		message = core.ReceiveMessage(conn)
		if message != nil {
			var s, _ = json.Marshal(*message)
			println(string(s))
			println("done")
			break
		} else {
			time.Sleep(time.Second)
		}
	}

	var uuid = uuid.NewV4()

	msg = core.Message{
		Cmd:     4,
		Seq:     29,
		Version: 1,
		Flag:    0,
		Body: &core.IMMessage{
			Sender:    1,
			Receiver:  2,
			Timestamp: 0,
			Msgid:     66,
			Content:   "{\"text\":\"test_client2\",\"uuid\":\"" + uuid.String() + "\"}",
		},
		Meta: nil,
	}
	e = core.SendMessage(conn, &msg)
	if e != nil {
		t.Fatal(e)
	}

	//var message *core.Message
	for {
		message = core.ReceiveMessage(conn)
		if message != nil {
			var s, _ = json.Marshal(*message)
			println(string(s))
			println("done")
			break
		} else {
			time.Sleep(time.Second)
		}
	}

}
