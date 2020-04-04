/**
 * Copyright (c) 2014-2015, GoBelieve
 * All rights reserved.
 *
 * This program is free software; you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation; either version 2 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program; if not, write to the Free Software
 * Foundation, Inc., 59 Temple Place, Suite 330, Boston, MA  02111-1307  USA
 */

package main

import (
	"github.com/GoBelieveIO/im_service/core"
	"net/http"
)
import "encoding/json"
import "time"
import "net/url"
import "strconv"
import "sync/atomic"
import log "github.com/golang/glog"
import "io/ioutil"
import "github.com/bitly/go-simplejson"

func SendGroupNotification(appid int64, gid int64,
	notification string, members core.IntSet) {

	msg := &core.Message{Cmd: core.MSG_GROUP_NOTIFICATION, Body: &core.GroupNotification{notification}}

	for member := range members {
		msgid, _, err := SaveMessage(appid, member, 0, msg)
		if err != nil {
			break
		}

		//发送同步的通知消息
		notify := &core.Message{Cmd: core.MSG_SYNC_NOTIFY, Body: &core.SyncNotify{SyncKey: msgid}}
		SendAppMessage(appid, member, notify)
	}
}

func SendSuperGroupNotification(appid int64, gid int64,
	notification string, members core.IntSet) {

	msg := &core.Message{Cmd: core.MSG_GROUP_NOTIFICATION, Body: &core.GroupNotification{notification}}

	msgid, _, err := SaveGroupMessage(appid, gid, 0, msg)

	if err != nil {
		log.Errorf("save group message: %d err:%s", gid, err)
		return
	}

	for member := range members {
		//发送同步的通知消息
		notify := &core.Message{Cmd: core.MSG_SYNC_GROUP_NOTIFY, Body: &core.GroupSyncNotify{gid, msgid}}
		SendAppMessage(appid, member, notify)
	}
}

func SendGroupIMMessage(im *core.IMMessage, appid int64) {
	m := &core.Message{Cmd: core.MSG_GROUP_IM, Version: core.DEFAULT_VERSION, Body: im}
	deliver := GetGroupMessageDeliver(im.Receiver)
	group := deliver.LoadGroup(im.Receiver)
	if group == nil {
		log.Warning("can't find group:", im.Receiver)
		return
	}
	if group.super {
		msgid, _, err := SaveGroupMessage(appid, im.Receiver, 0, m)
		if err != nil {
			return
		}

		//推送外部通知
		PushGroupMessage(appid, group, m)

		//发送同步的通知消息
		notify := &core.Message{Cmd: core.MSG_SYNC_GROUP_NOTIFY, Body: &core.GroupSyncNotify{GroupId: im.Receiver, SyncKey: msgid}}
		SendAppGroupMessage(appid, group, notify)

	} else {
		gm := &core.PendingGroupMessage{}
		gm.Appid = appid
		gm.Sender = im.Sender
		gm.DeviceID = 0
		gm.Gid = im.Receiver
		gm.Timestamp = im.Timestamp
		members := group.Members()
		gm.Members = make([]int64, len(members))
		i := 0
		for uid := range members {
			gm.Members[i] = uid
			i += 1
		}

		gm.Content = im.Content
		deliver := GetGroupMessageDeliver(group.gid)
		m := &core.Message{Cmd: core.MSG_PENDING_GROUP_MESSAGE, Body: gm}
		deliver.SaveMessage(m, nil)
	}
	atomic.AddInt64(&server_summary.in_message_count, 1)
}

func SendIMMessage(im *core.IMMessage, appid int64) {
	m := &core.Message{Cmd: core.MSG_IM, Version: core.DEFAULT_VERSION, Body: im}
	msgid, _, err := SaveMessage(appid, im.Receiver, 0, m)
	if err != nil {
		return
	}

	//保存到发送者自己的消息队列
	msgid2, _, err := SaveMessage(appid, im.Sender, 0, m)
	if err != nil {
		return
	}

	//推送外部通知
	PushMessage(appid, im.Receiver, m)

	//发送同步的通知消息
	notify := &core.Message{Cmd: core.MSG_SYNC_NOTIFY, Body: &core.SyncNotify{SyncKey: msgid}}
	SendAppMessage(appid, im.Receiver, notify)

	//发送同步的通知消息
	notify = &core.Message{Cmd: core.MSG_SYNC_NOTIFY, Body: &core.SyncNotify{SyncKey: msgid2}}
	SendAppMessage(appid, im.Sender, notify)

	atomic.AddInt64(&server_summary.in_message_count, 1)
}

//http
func PostGroupNotification(w http.ResponseWriter, req *http.Request) {
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		WriteHttpError(400, err.Error(), w)
		return
	}

	obj, err := simplejson.NewJson(body)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid json format", w)
		return
	}

	appid, err := obj.Get("appid").Int64()
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid json format", w)
		return
	}
	group_id, err := obj.Get("group_id").Int64()
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid json format", w)
		return
	}

	notification, err := obj.Get("notification").String()
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid json format", w)
		return
	}

	is_super := false
	if super_json, ok := obj.CheckGet("super"); ok {
		is_super, err = super_json.Bool()
		if err != nil {
			log.Info("error:", err)
			WriteHttpError(400, "invalid json format", w)
			return
		}
	}

	members := core.NewIntSet()
	marray, err := obj.Get("members").Array()
	for _, m := range marray {
		if _, ok := m.(json.Number); ok {
			member, err := m.(json.Number).Int64()
			if err != nil {
				log.Info("error:", err)
				WriteHttpError(400, "invalid json format", w)
				return
			}
			members.Add(member)
		}
	}

	deliver := GetGroupMessageDeliver(group_id)
	group := deliver.LoadGroup(group_id)
	if group != nil {
		ms := group.Members()
		for m, _ := range ms {
			members.Add(m)
		}
		is_super = group.super
	}

	if len(members) == 0 {
		WriteHttpError(400, "group no member", w)
		return
	}
	if is_super {
		SendSuperGroupNotification(appid, group_id, notification, members)
	} else {
		SendGroupNotification(appid, group_id, notification, members)
	}

	log.Info("post group notification success")
	w.WriteHeader(200)
}

func PostPeerMessage(w http.ResponseWriter, req *http.Request) {
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		WriteHttpError(400, err.Error(), w)
		return
	}

	m, _ := url.ParseQuery(req.URL.RawQuery)

	appid, err := strconv.ParseInt(m.Get("appid"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid param", w)
		return
	}

	sender, err := strconv.ParseInt(m.Get("sender"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid param", w)
		return
	}

	receiver, err := strconv.ParseInt(m.Get("receiver"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid param", w)
		return
	}

	content := string(body)

	im := &core.IMMessage{}
	im.Sender = sender
	im.Receiver = receiver
	im.Msgid = 0
	im.Timestamp = int32(time.Now().Unix())
	im.Content = content

	SendIMMessage(im, appid)

	w.WriteHeader(200)
	log.Info("post peer im message success")
}

func PostGroupMessage(w http.ResponseWriter, req *http.Request) {
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		WriteHttpError(400, err.Error(), w)
		return
	}

	m, _ := url.ParseQuery(req.URL.RawQuery)

	appid, err := strconv.ParseInt(m.Get("appid"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid param", w)
		return
	}

	sender, err := strconv.ParseInt(m.Get("sender"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid param", w)
		return
	}

	receiver, err := strconv.ParseInt(m.Get("receiver"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid param", w)
		return
	}

	content := string(body)

	im := &core.IMMessage{}
	im.Sender = sender
	im.Receiver = receiver
	im.Msgid = 0
	im.Timestamp = int32(time.Now().Unix())
	im.Content = content

	SendGroupIMMessage(im, appid)

	w.WriteHeader(200)

	log.Info("post group im message success")
}

func LoadLatestMessage(w http.ResponseWriter, req *http.Request) {
	log.Info("load latest message")
	m, _ := url.ParseQuery(req.URL.RawQuery)

	appid, err := strconv.ParseInt(m.Get("appid"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid query param", w)
		return
	}

	uid, err := strconv.ParseInt(m.Get("uid"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid query param", w)
		return
	}

	limit, err := strconv.ParseInt(m.Get("limit"), 10, 32)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid query param", w)
		return
	}
	log.Infof("appid:%d uid:%d limit:%d", appid, uid, limit)

	rpc := GetStorageRPCClient(uid)

	s := &core.HistoryRequest{
		AppID: appid,
		Uid:   uid,
		Limit: int32(limit),
	}

	resp, err := rpc.Call("GetLatestMessage", s)
	if err != nil {
		log.Warning("get latest message err:", err)
		WriteHttpError(400, "internal error", w)
		return
	}

	hm := resp.([]*core.HistoryMessage)
	messages := make([]*core.EMessage, 0)
	for _, msg := range hm {
		m := &core.Message{Cmd: int(msg.Cmd), Version: core.DEFAULT_VERSION}
		m.FromData(msg.Raw)
		e := &core.EMessage{MsgId: msg.MsgID, DeviceId: msg.DeviceID, Msg: m}
		messages = append(messages, e)
	}

	if len(messages) > 0 {
		//reverse
		size := len(messages)
		for i := 0; i < size/2; i++ {
			t := messages[i]
			messages[i] = messages[size-i-1]
			messages[size-i-1] = t
		}
	}

	msg_list := make([]map[string]interface{}, 0, len(messages))
	for _, emsg := range messages {
		if emsg.Msg.Cmd == core.MSG_IM ||
			emsg.Msg.Cmd == core.MSG_GROUP_IM {
			im := emsg.Msg.Body.(*core.IMMessage)

			obj := make(map[string]interface{})
			obj["content"] = im.Content
			obj["timestamp"] = im.Timestamp
			obj["sender"] = im.Sender
			obj["receiver"] = im.Receiver
			obj["command"] = emsg.Msg.Cmd
			obj["id"] = emsg.MsgId
			msg_list = append(msg_list, obj)

		} else if emsg.Msg.Cmd == core.MSG_CUSTOMER ||
			emsg.Msg.Cmd == core.MSG_CUSTOMER_SUPPORT {
			im := emsg.Msg.Body.(*core.CustomerMessage)

			obj := make(map[string]interface{})
			obj["content"] = im.Content
			obj["timestamp"] = im.Timestamp
			obj["customer_appid"] = im.CustomerAppid
			obj["customer_id"] = im.CustomerId
			obj["store_id"] = im.StoreId
			obj["seller_id"] = im.SellerId
			obj["command"] = emsg.Msg.Cmd
			obj["id"] = emsg.MsgId
			msg_list = append(msg_list, obj)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	obj := make(map[string]interface{})
	obj["data"] = msg_list
	b, _ := json.Marshal(obj)
	w.Write(b)
	log.Info("load latest message success")
}

func LoadHistoryMessage(w http.ResponseWriter, req *http.Request) {
	log.Info("load message")
	m, _ := url.ParseQuery(req.URL.RawQuery)

	appid, err := strconv.ParseInt(m.Get("appid"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid query param", w)
		return
	}

	uid, err := strconv.ParseInt(m.Get("uid"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid query param", w)
		return
	}

	msgid, err := strconv.ParseInt(m.Get("last_id"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid query param", w)
		return
	}

	rpc := GetStorageRPCClient(uid)

	s := &core.SyncHistory{
		AppID:     appid,
		Uid:       uid,
		DeviceID:  0,
		LastMsgID: msgid,
	}

	resp, err := rpc.Call("SyncMessage", s)
	if err != nil {
		log.Warning("sync message err:", err)
		return
	}

	ph := resp.(*core.PeerHistoryMessage)
	messages := ph.Messages

	if len(messages) > 0 {
		//reverse
		size := len(messages)
		for i := 0; i < size/2; i++ {
			t := messages[i]
			messages[i] = messages[size-i-1]
			messages[size-i-1] = t
		}
	}

	msg_list := make([]map[string]interface{}, 0, len(messages))
	for _, emsg := range messages {
		msg := &core.Message{Cmd: int(emsg.Cmd), Version: core.DEFAULT_VERSION}
		msg.FromData(emsg.Raw)
		if msg.Cmd == core.MSG_IM ||
			msg.Cmd == core.MSG_GROUP_IM {
			im := msg.Body.(*core.IMMessage)

			obj := make(map[string]interface{})
			obj["content"] = im.Content
			obj["timestamp"] = im.Timestamp
			obj["sender"] = im.Sender
			obj["receiver"] = im.Receiver
			obj["command"] = emsg.Cmd
			obj["id"] = emsg.MsgID
			msg_list = append(msg_list, obj)

		} else if msg.Cmd == core.MSG_CUSTOMER ||
			msg.Cmd == core.MSG_CUSTOMER_SUPPORT {
			im := msg.Body.(*core.CustomerMessage)

			obj := make(map[string]interface{})
			obj["content"] = im.Content
			obj["timestamp"] = im.Timestamp
			obj["customer_appid"] = im.CustomerAppid
			obj["customer_id"] = im.CustomerId
			obj["store_id"] = im.StoreId
			obj["seller_id"] = im.SellerId
			obj["command"] = emsg.Cmd
			obj["id"] = emsg.MsgID
			msg_list = append(msg_list, obj)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	obj := make(map[string]interface{})
	obj["data"] = msg_list
	b, _ := json.Marshal(obj)
	w.Write(b)
	log.Info("load history message success")
}

func GetOfflineCount(w http.ResponseWriter, req *http.Request) {
	m, _ := url.ParseQuery(req.URL.RawQuery)

	appid, err := strconv.ParseInt(m.Get("appid"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid query param", w)
		return
	}

	uid, err := strconv.ParseInt(m.Get("uid"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid query param", w)
		return
	}

	last_id, err := strconv.ParseInt(m.Get("sync_key"), 10, 64)
	if err != nil || last_id == 0 {
		last_id = GetSyncKey(appid, uid)
	}
	sync_key := core.SyncHistory{AppID: appid, Uid: uid, LastMsgID: last_id}

	dc := GetStorageRPCClient(uid)

	resp, err := dc.Call("GetNewCount", sync_key)

	if err != nil {
		log.Warning("get new count err:", err)
		WriteHttpError(500, "server internal error", w)
		return
	}
	count := resp.(int64)

	log.Infof("get offline appid:%d uid:%d sync_key:%d count:%d", appid, uid, last_id, count)
	obj := make(map[string]interface{})
	obj["count"] = count
	WriteHttpObj(obj, w)
}

func SendNotification(w http.ResponseWriter, req *http.Request) {
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		WriteHttpError(400, err.Error(), w)
		return
	}

	m, _ := url.ParseQuery(req.URL.RawQuery)

	appid, err := strconv.ParseInt(m.Get("appid"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid query param", w)
		return
	}

	uid, err := strconv.ParseInt(m.Get("uid"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid query param", w)
		return
	}
	sys := &core.SystemMessage{string(body)}
	msg := &core.Message{Cmd: core.MSG_NOTIFICATION, Body: sys}
	SendAppMessage(appid, uid, msg)

	w.WriteHeader(200)
}

func SendSystemMessage(w http.ResponseWriter, req *http.Request) {
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		WriteHttpError(400, err.Error(), w)
		return
	}

	m, _ := url.ParseQuery(req.URL.RawQuery)

	appid, err := strconv.ParseInt(m.Get("appid"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid query param", w)
		return
	}

	uid, err := strconv.ParseInt(m.Get("uid"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid query param", w)
		return
	}
	sys := &core.SystemMessage{string(body)}
	msg := &core.Message{Cmd: core.MSG_SYSTEM, Body: sys}

	msgid, _, err := SaveMessage(appid, uid, 0, msg)
	if err != nil {
		WriteHttpError(500, "internal server error", w)
		return
	}

	//推送通知
	PushMessage(appid, uid, msg)

	//发送同步的通知消息
	notify := &core.Message{Cmd: core.MSG_SYNC_NOTIFY, Body: &core.SyncNotify{SyncKey: msgid}}
	SendAppMessage(appid, uid, notify)

	w.WriteHeader(200)
}

func SendRoomMessage(w http.ResponseWriter, req *http.Request) {
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		WriteHttpError(400, err.Error(), w)
		return
	}

	m, _ := url.ParseQuery(req.URL.RawQuery)

	appid, err := strconv.ParseInt(m.Get("appid"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid query param", w)
		return
	}

	uid, err := strconv.ParseInt(m.Get("uid"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid query param", w)
		return
	}
	room_id, err := strconv.ParseInt(m.Get("room"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid query param", w)
		return
	}

	room_im := &core.RoomMessage{new(core.RTMessage)}
	room_im.Sender = uid
	room_im.Receiver = room_id
	room_im.Content = string(body)

	msg := &core.Message{Cmd: core.MSG_ROOM_IM, Body: room_im}

	DispatchMessageToRoom(msg, room_id, appid, nil)

	amsg := &core.AppMessage{Appid: appid, Receiver: room_id, Msg: msg}
	channel := GetRoomChannel(room_id)
	channel.PublishRoom(amsg)

	w.WriteHeader(200)
}

func SendCustomerSupportMessage(w http.ResponseWriter, req *http.Request) {
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		WriteHttpError(400, err.Error(), w)
		return
	}

	obj, err := simplejson.NewJson(body)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid json format", w)
		return
	}

	customer_appid, err := obj.Get("customer_appid").Int64()
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid json format", w)
		return
	}

	customer_id, err := obj.Get("customer_id").Int64()
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid json format", w)
		return
	}

	store_id, err := obj.Get("store_id").Int64()
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid json format", w)
		return
	}

	seller_id, err := obj.Get("seller_id").Int64()
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid json format", w)
		return
	}

	content, err := obj.Get("content").String()
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid json format", w)
		return
	}

	cm := &core.CustomerMessage{}
	cm.CustomerAppid = customer_appid
	cm.CustomerId = customer_id
	cm.StoreId = store_id
	cm.SellerId = seller_id
	cm.Content = content
	cm.Timestamp = int32(time.Now().Unix())

	m := &core.Message{Cmd: core.MSG_CUSTOMER_SUPPORT, Body: cm}

	msgid, _, err := SaveMessage(cm.CustomerAppid, cm.CustomerId, 0, m)
	if err != nil {
		log.Warning("save message error:", err)
		WriteHttpError(500, "internal server error", w)
		return
	}

	msgid2, _, err := SaveMessage(config.kefu_appid, cm.SellerId, 0, m)
	if err != nil {
		log.Warning("save message error:", err)
		WriteHttpError(500, "internal server error", w)
		return
	}

	PushMessage(cm.CustomerAppid, cm.CustomerId, m)

	//发送给自己的其它登录点
	notify := &core.Message{Cmd: core.MSG_SYNC_NOTIFY, Body: &core.SyncNotify{SyncKey: msgid2}}
	SendAppMessage(config.kefu_appid, cm.SellerId, notify)

	//发送同步的通知消息
	notify = &core.Message{Cmd: core.MSG_SYNC_NOTIFY, Body: &core.SyncNotify{SyncKey: msgid}}
	SendAppMessage(cm.CustomerAppid, cm.CustomerId, notify)

	w.WriteHeader(200)
}

func SendCustomerMessage(w http.ResponseWriter, req *http.Request) {
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		WriteHttpError(400, err.Error(), w)
		return
	}

	obj, err := simplejson.NewJson(body)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid json format", w)
		return
	}

	customer_appid, err := obj.Get("customer_appid").Int64()
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid json format", w)
		return
	}

	customer_id, err := obj.Get("customer_id").Int64()
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid json format", w)
		return
	}

	store_id, err := obj.Get("store_id").Int64()
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid json format", w)
		return
	}

	seller_id, err := obj.Get("seller_id").Int64()
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid json format", w)
		return
	}

	content, err := obj.Get("content").String()
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid json format", w)
		return
	}

	cm := &core.CustomerMessage{}
	cm.CustomerAppid = customer_appid
	cm.CustomerId = customer_id
	cm.StoreId = store_id
	cm.SellerId = seller_id
	cm.Content = content
	cm.Timestamp = int32(time.Now().Unix())

	m := &core.Message{Cmd: core.MSG_CUSTOMER, Body: cm}

	msgid, _, err := SaveMessage(config.kefu_appid, cm.SellerId, 0, m)
	if err != nil {
		log.Warning("save message error:", err)
		WriteHttpError(500, "internal server error", w)
		return
	}
	msgid2, _, err := SaveMessage(cm.CustomerAppid, cm.CustomerId, 0, m)
	if err != nil {
		log.Warning("save message error:", err)
		WriteHttpError(500, "internal server error", w)
		return
	}

	PushMessage(config.kefu_appid, cm.SellerId, m)

	//发送同步的通知消息
	notify := &core.Message{Cmd: core.MSG_SYNC_NOTIFY, Body: &core.SyncNotify{SyncKey: msgid}}
	SendAppMessage(config.kefu_appid, cm.SellerId, notify)

	//发送给自己的其它登录点
	notify = &core.Message{Cmd: core.MSG_SYNC_NOTIFY, Body: &core.SyncNotify{SyncKey: msgid2}}
	SendAppMessage(cm.CustomerAppid, cm.CustomerId, notify)

	resp := make(map[string]interface{})
	resp["seller_id"] = seller_id
	WriteHttpObj(resp, w)
}

func SendRealtimeMessage(w http.ResponseWriter, req *http.Request) {
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		WriteHttpError(400, err.Error(), w)
		return
	}

	m, _ := url.ParseQuery(req.URL.RawQuery)

	appid, err := strconv.ParseInt(m.Get("appid"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid query param", w)
		return
	}

	sender, err := strconv.ParseInt(m.Get("sender"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid query param", w)
		return
	}
	receiver, err := strconv.ParseInt(m.Get("receiver"), 10, 64)
	if err != nil {
		log.Info("error:", err)
		WriteHttpError(400, "invalid query param", w)
		return
	}

	rt := &core.RTMessage{}
	rt.Sender = sender
	rt.Receiver = receiver
	rt.Content = string(body)

	msg := &core.Message{Cmd: core.MSG_RT, Body: rt}
	SendAppMessage(appid, receiver, msg)
	w.WriteHeader(200)
}
