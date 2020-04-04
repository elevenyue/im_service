package main

import (
	"github.com/GoBelieveIO/im_service/core"
	"testing"
)
import "log"
import "fmt"
import "os"
import "flag"

var appid int64 = 0
var device_id int64 = 0

func init() {
	master = NewMaster()
	master.Start()

	config = &StorageConfig{}
}

func TestMain(m *testing.M) {
	flag.Parse()
	storage = NewStorage("/tmp")
	os.Exit(m.Run())
}

func Test_Storage(t *testing.T) {
	im := &core.IMMessage{Sender: 1, Receiver: 2, Content: "test"}
	msg := &core.Message{Cmd: core.MSG_IM, Body: im}
	msgid := storage.SaveMessage(msg)
	msg2 := storage.LoadMessage(msgid)
	if msg2 != nil {
		log.Println("msg2 cmd:", core.Command(msg2.Cmd))
	} else {
		log.Println("can't load msg:", msgid)
	}
}

func Test_LoadLatest(t *testing.T) {
	im := &core.IMMessage{Sender: 1, Receiver: 2, Content: "test"}
	msg := &core.Message{Cmd: core.MSG_IM, Body: im}
	storage.SavePeerMessage(appid, im.Receiver, device_id, msg)

	im = &core.IMMessage{Sender: 1, Receiver: 2, Content: "test2"}
	msg = &core.Message{Cmd: core.MSG_IM, Body: im}
	storage.SavePeerMessage(appid, im.Receiver, device_id, msg)

	messages := storage.LoadLatestMessages(appid, im.Receiver, 2)
	latest := messages[0]
	im2 := latest.Msg.Body.(*core.IMMessage)
	log.Println("sender:", im2.Sender, " receiver:", im2.Receiver, " content:", string(im2.Content))

	latest = messages[1]
	im2 = latest.Msg.Body.(*core.IMMessage)
	log.Println("sender:", im2.Sender, " receiver:", im2.Receiver, " content:", string(im2.Content))

}

func Test_Sync(t *testing.T) {
	last_id, _ := storage.GetLastMessageID(appid, 2)

	im := &core.IMMessage{Sender: 1, Receiver: 2, Content: "test"}
	msg := &core.Message{Cmd: core.MSG_IM, Body: im}
	storage.SavePeerMessage(appid, im.Receiver, device_id, msg)

	im = &core.IMMessage{Sender: 1, Receiver: 2, Content: "test2"}
	msg = &core.Message{Cmd: core.MSG_IM, Body: im}
	storage.SavePeerMessage(appid, im.Receiver, device_id, msg)

	messages, _, _ := storage.LoadHistoryMessagesV3(appid, im.Receiver, last_id, 0, 0)
	latest := messages[0]
	im2 := latest.Msg.Body.(*core.IMMessage)
	log.Println("sender:", im2.Sender, " receiver:", im2.Receiver, " content:", string(im2.Content))

	latest = messages[1]
	im2 = latest.Msg.Body.(*core.IMMessage)
	log.Println("sender:", im2.Sender, " receiver:", im2.Receiver, " content:", string(im2.Content))
}

func Test_SyncBatch(t *testing.T) {
	receiver := int64(2)
	last_id, _ := storage.GetLastMessageID(appid, receiver)

	for i := 0; i < 5000; i++ {
		content := fmt.Sprintf("test:%d", i)
		im := &core.IMMessage{Sender: 1, Receiver: receiver, Content: content}
		msg := &core.Message{Cmd: core.MSG_IM, Body: im}
		storage.SavePeerMessage(appid, im.Receiver, device_id, msg)
	}

	hasMore := true
	loop := 0
	for hasMore {
		messages, last_msgid, m := storage.LoadHistoryMessagesV3(appid, receiver, last_id, 1000, 4000)
		latest := messages[0]
		im2 := latest.Msg.Body.(*core.IMMessage)
		log.Println("loop:", loop, "sender:", im2.Sender, " receiver:", im2.Receiver, " content:", string(im2.Content))

		loop++
		last_id = last_msgid
		hasMore = m
	}
}

func Test_PeerIndex(t *testing.T) {
	storage.flushIndex()
}

func Test_NewCount(t *testing.T) {
	receiver := int64(2)

	content := "test"
	im := &core.IMMessage{Sender: 1, Receiver: receiver, Content: content}
	msg := &core.Message{Cmd: core.MSG_IM, Body: im}
	last_id, _ := storage.SavePeerMessage(appid, im.Receiver, device_id, msg)

	for i := 0; i < 5000; i++ {
		content = fmt.Sprintf("test:%d", i)
		im = &core.IMMessage{Sender: 1, Receiver: receiver, Content: content}
		msg = &core.Message{Cmd: core.MSG_IM, Body: im}
		storage.SavePeerMessage(appid, im.Receiver, device_id, msg)
	}

	count := storage.GetNewCount(appid, receiver, last_id)
	if count != 5000 {
		t.Errorf("new count = %d; expected %d", count, 5000)
	} else {
		log.Println("last id:", last_id, " new count:", count)
	}

	count = storage.GetNewCount(appid, receiver, 0)
	log.Println("last id:", 0, " new count:", count)
}
