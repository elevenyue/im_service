package tests

import (
	"encoding/json"
	"github.com/GoBelieveIO/im_service/core"
	"github.com/gomodule/redigo/redis"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"
)

func TestInitRedis(t *testing.T) {
	var redisUrl = "127.0.0.1:6379"
	c, e := redis.Dial("tcp", redisUrl)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.Do("SET", "devices_87c6f75ef5d3a93d_2", 1)
	c.Do("SET", "devices_id", 1)
}

func TestClientMany(t *testing.T) {
	var w = sync.WaitGroup{}
	for i := 0; i < 10000; i++ {
		time.Sleep(time.Millisecond)
		w.Add(i)
		go func() {
			TestClient(t)
			w.Done()
		}()
	}
	w.Wait()
	println("All Done!")
}

//请先修改redisURL为redis服务器地址,并且执行 TestInitRedis 插入初始化数据
func TestClient(t *testing.T) {
	//init
	conn, e := net.Dial("tcp", "127.0.0.1:23000")
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()

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
			println("loading...")
		}
	}

	msg = core.Message{
		Cmd:     4,
		Seq:     rand.Intn(100000),
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
	//for {
	//	message = core.ReceiveMessage(conn)
	//	if message != nil {
	//		var s, _ = json.Marshal(*message)
	//		println(string(s))
	//		break
	//	} else {
	//		time.Sleep(time.Second)
	//		println("loading...")
	//	}
	//}
	//println("done")
}
