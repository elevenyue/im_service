package tests

import (
	"encoding/json"
	"fmt"
	"github.com/GoBelieveIO/im_service/core"
	"github.com/gomodule/redigo/redis"
	"net"
	"testing"
	"time"
)

//请修改redisURL为redis服务器地址
func TestClient(t *testing.T) {
	if true {
		fmt.Println("you need disable this code")
		return
	}
	//init
	var redisUrl = "127.0.0.1:6379"
	c, e := redis.Dial("tcp", redisUrl)
	if e != nil {
		t.Fatal(e)
	}
	c.Do("SET", "devices_87c6f75ef5d3a93d_2", 1)
	c.Do("SET", "devices_id", 1)

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
			Content:   "{\"text\":\"test_client3\",\"uuid\":\"d4ee8dd0-a1f4-4018-b261-5c7498a3244c\"}",
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
