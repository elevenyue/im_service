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
	"time"
)
import "sync/atomic"
import log "github.com/golang/glog"

type PeerClient struct {
	*Connection
}

func (client *PeerClient) Login() {
	channel := GetChannel(client.uid)

	channel.Subscribe(client.appid, client.uid, client.online)

	for _, c := range group_route_channels {
		if c == channel {
			continue
		}

		c.Subscribe(client.appid, client.uid, client.online)
	}

	SetUserUnreadCount(client.appid, client.uid, 0)
}

func (client *PeerClient) Logout() {
	if client.uid > 0 {
		channel := GetChannel(client.uid)
		channel.Unsubscribe(client.appid, client.uid, client.online)

		for _, c := range group_route_channels {
			if c == channel {
				continue
			}

			c.Unsubscribe(client.appid, client.uid, client.online)
		}
	}
}

func (client *PeerClient) HandleSync(sync_key *core.SyncKey) {
	if client.uid == 0 {
		return
	}
	last_id := sync_key.SyncKey

	if last_id == 0 {
		last_id = GetSyncKey(client.appid, client.uid)
	}

	rpc := GetStorageRPCClient(client.uid)

	s := &core.SyncHistory{
		AppID:     client.appid,
		Uid:       client.uid,
		DeviceID:  client.device_ID,
		LastMsgID: last_id,
	}

	log.Infof("syncing message:%d %d %d %d", client.appid, client.uid, client.device_ID, last_id)

	resp, err := rpc.Call("SyncMessage", s)
	if err != nil {
		log.Warning("sync message err:", err)
		return
	}
	client.sync_count += 1

	ph := resp.(*core.PeerHistoryMessage)
	messages := ph.Messages

	msgs := make([]*core.Message, 0, len(messages)+2)

	sk := &core.SyncKey{last_id}
	msgs = append(msgs, &core.Message{Cmd: core.MSG_SYNC_BEGIN, Body: sk})

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		log.Info("message:", msg.MsgID, core.Command(msg.Cmd))
		m := &core.Message{Cmd: int(msg.Cmd), Version: core.DEFAULT_VERSION}
		m.FromData(msg.Raw)
		sk.SyncKey = msg.MsgID
		if client.isSender(m, msg.DeviceID) {
			m.Flag |= core.MESSAGE_FLAG_SELF
		}
		msgs = append(msgs, m)
	}

	if ph.LastMsgID < last_id && ph.LastMsgID > 0 {
		sk.SyncKey = ph.LastMsgID
		log.Warningf("client last id:%d server last id:%d", last_id, ph.LastMsgID)
	}

	msgs = append(msgs, &core.Message{Cmd: core.MSG_SYNC_END, Body: sk})

	client.EnqueueMessages(msgs)

	if ph.HasMore {
		notify := &core.Message{Cmd: core.MSG_SYNC_NOTIFY, Body: &core.SyncKey{ph.LastMsgID + 1}}
		client.EnqueueMessage(notify)
	}
}

func (client *PeerClient) HandleSyncKey(sync_key *core.SyncKey) {
	if client.uid == 0 {
		return
	}

	last_id := sync_key.SyncKey
	log.Infof("sync key:%d %d %d %d", client.appid, client.uid, client.device_ID, last_id)
	if last_id > 0 {
		s := &core.SyncHistory{
			AppID:     client.appid,
			Uid:       client.uid,
			LastMsgID: last_id,
		}
		sync_c <- s
	}
}

func (client *PeerClient) HandleIMMessage(message *core.Message) {
	msg := message.Body.(*core.IMMessage)
	seq := message.Seq
	if client.uid == 0 {
		log.Warning("client has't been authenticated")
		return
	}

	if msg.Sender != client.uid {
		log.Warningf("im message sender:%d client uid:%d\n", msg.Sender, client.uid)
		return
	}

	var rs Relationship = NoneRelationship
	if config.friend_permission || config.enable_blacklist {
		rs = relationship_pool.GetRelationship(client.appid, client.uid, msg.Receiver)
	}
	if config.friend_permission {
		rs := relationship_pool.GetRelationship(client.appid, client.uid, msg.Receiver)
		if !rs.IsMyFriend() {
			ack := &core.Message{Cmd: core.MSG_ACK, Version: client.version, Body: &core.MessageACK{Seq: int32(seq), Status: core.ACK_NOT_MY_FRIEND}}
			client.EnqueueMessage(ack)
			log.Infof("relationship%d-%d:%d invalid, can't send message", msg.Sender, msg.Receiver, rs)
			return
		}

		if !rs.IsYourFriend() {
			ack := &core.Message{Cmd: core.MSG_ACK, Version: client.version, Body: &core.MessageACK{Seq: int32(seq), Status: core.ACK_NOT_YOUR_FRIEND}}
			client.EnqueueMessage(ack)
			log.Infof("relationship%d-%d:%d invalid, can't send message", msg.Sender, msg.Receiver, rs)
			return
		}
	}
	if config.enable_blacklist {
		if rs.IsInYourBlacklist() {
			ack := &core.Message{Cmd: core.MSG_ACK, Version: client.version, Body: &core.MessageACK{Seq: int32(seq), Status: core.ACK_IN_YOUR_BLACKLIST}}
			client.EnqueueMessage(ack)
			log.Infof("relationship%d-%d:%d invalid, can't send message", msg.Sender, msg.Receiver, rs)
			return
		}
	}

	if message.Flag&core.MESSAGE_FLAG_TEXT != 0 {
		FilterDirtyWord(msg)
	}
	msg.Timestamp = int32(time.Now().Unix())
	m := &core.Message{Cmd: core.MSG_IM, Version: core.DEFAULT_VERSION, Body: msg}

	msgid, prev_msgid, err := SaveMessage(client.appid, msg.Receiver, client.device_ID, m)
	if err != nil {
		log.Errorf("save peer message:%d %d err:%d", msg.Sender, msg.Receiver, err)
		return
	}

	//保存到自己的消息队列，这样用户的其它登陆点也能接受到自己发出的消息
	msgid2, prev_msgid2, err := SaveMessage(client.appid, msg.Sender, client.device_ID, m)
	if err != nil {
		log.Errorf("save peer message:%d %d err:%d", msg.Sender, msg.Receiver, err)
		return
	}

	//推送外部通知
	PushMessage(client.appid, msg.Receiver, m)

	meta := &core.Metadata{SyncKey: msgid, PrevSyncKey: prev_msgid}
	m1 := &core.Message{Cmd: core.MSG_IM, Version: core.DEFAULT_VERSION, Flag: message.Flag | core.MESSAGE_FLAG_PUSH, Body: msg, Meta: meta}
	client.SendMessage(msg.Receiver, m1)
	notify := &core.Message{Cmd: core.MSG_SYNC_NOTIFY, Body: &core.SyncKey{msgid}}
	client.SendMessage(msg.Receiver, notify)

	//发送给自己的其它登录点
	meta = &core.Metadata{SyncKey: msgid2, PrevSyncKey: prev_msgid2}
	m2 := &core.Message{Cmd: core.MSG_IM, Version: core.DEFAULT_VERSION, Flag: message.Flag | core.MESSAGE_FLAG_PUSH, Body: msg, Meta: meta}
	client.SendMessage(client.uid, m2)
	notify = &core.Message{Cmd: core.MSG_SYNC_NOTIFY, Body: &core.SyncKey{msgid}}
	client.SendMessage(client.uid, notify)

	meta = &core.Metadata{SyncKey: msgid2, PrevSyncKey: prev_msgid2}
	ack := &core.Message{Cmd: core.MSG_ACK, Body: &core.MessageACK{Seq: int32(seq)}, Meta: meta}
	r := client.EnqueueMessage(ack)
	if !r {
		log.Warning("send peer message ack error")
	}

	atomic.AddInt64(&server_summary.in_message_count, 1)
	log.Infof("peer message sender:%d receiver:%d msgid:%d\n", msg.Sender, msg.Receiver, msgid)
}

func (client *PeerClient) HandleUnreadCount(u *core.MessageUnreadCount) {
	SetUserUnreadCount(client.appid, client.uid, u.Count)
}

func (client *PeerClient) HandleRTMessage(msg *core.Message) {
	rt := msg.Body.(*core.RTMessage)
	if rt.Sender != client.uid {
		log.Warningf("rt message sender:%d client uid:%d\n", rt.Sender, client.uid)
		return
	}

	m := &core.Message{Cmd: core.MSG_RT, Body: rt}
	client.SendMessage(rt.Receiver, m)

	atomic.AddInt64(&server_summary.in_message_count, 1)
	log.Infof("realtime message sender:%d receiver:%d", rt.Sender, rt.Receiver)
}

func (client *PeerClient) HandleMessage(msg *core.Message) {
	switch msg.Cmd {
	case core.MSG_IM:
		client.HandleIMMessage(msg)
	case core.MSG_RT:
		client.HandleRTMessage(msg)
	case core.MSG_UNREAD_COUNT:
		client.HandleUnreadCount(msg.Body.(*core.MessageUnreadCount))
	case core.MSG_SYNC:
		client.HandleSync(msg.Body.(*core.SyncKey))
	case core.MSG_SYNC_KEY:
		client.HandleSyncKey(msg.Body.(*core.SyncKey))
	}
}
