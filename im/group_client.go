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

package im

import (
	"github.com/GoBelieveIO/im_service/core"
	"time"
)
import "sync/atomic"
import "errors"
import log "github.com/golang/glog"

type GroupClient struct {
	*Connection
}

func (client *GroupClient) HandleSuperGroupMessage(msg *core.IMMessage, group *Group) (int64, int64, error) {
	m := &core.Message{Cmd: core.MSG_GROUP_IM, Version: core.DEFAULT_VERSION, Body: msg}
	msgid, prev_msgid, err := SaveGroupMessage(client.appid, msg.Receiver, client.device_ID, m)
	if err != nil {
		log.Errorf("save group message:%d %d err:%s", msg.Sender, msg.Receiver, err)
		return 0, 0, err
	}

	//推送外部通知
	PushGroupMessage(client.appid, group, m)

	m.Meta = &core.Metadata{SyncKey: msgid, PrevSyncKey: prev_msgid}
	m.Flag = core.MESSAGE_FLAG_PUSH | core.MESSAGE_FLAG_SUPER_GROUP
	client.SendGroupMessage(group, m)

	notify := &core.Message{Cmd: core.MSG_SYNC_GROUP_NOTIFY, Body: &core.GroupSyncKey{GroupId: msg.Receiver, SyncKey: msgid}}
	client.SendGroupMessage(group, notify)

	return msgid, prev_msgid, nil
}

func (client *GroupClient) HandleGroupMessage(im *core.IMMessage, group *Group) (int64, int64, error) {
	gm := &core.PendingGroupMessage{}
	gm.Appid = client.appid
	gm.Sender = im.Sender
	gm.DeviceID = client.device_ID
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

	c := make(chan *core.Metadata, 1)
	callback_id := deliver.SaveMessage(m, c)
	defer deliver.RemoveCallback(callback_id)
	select {
	case meta := <-c:
		return meta.SyncKey, meta.PrevSyncKey, nil
	case <-time.After(time.Second * 2):
		log.Errorf("save group message:%d %d timeout", im.Sender, im.Receiver)
		return 0, 0, errors.New("timeout")
	}
}

func (client *GroupClient) HandleGroupIMMessage(message *core.Message) {
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
	if message.Flag&core.MESSAGE_FLAG_TEXT != 0 {
		FilterDirtyWord(msg)
	}

	msg.Timestamp = int32(time.Now().Unix())

	deliver := GetGroupMessageDeliver(msg.Receiver)
	group := deliver.LoadGroup(msg.Receiver)
	if group == nil {
		log.Warning("can't find group:", msg.Receiver)
		return
	}

	if !group.IsMember(msg.Sender) {
		ack := &core.Message{Cmd: core.MSG_ACK, Body: &core.MessageACK{Seq: int32(seq), Status: core.ACK_NOT_GROUP_MEMBER}}
		client.EnqueueMessage(ack)
		log.Warningf("sender:%d is not group member", msg.Sender)
		return
	}

	if group.GetMemberMute(msg.Sender) {
		log.Warningf("sender:%d is mute in group", msg.Sender)
		return
	}

	var meta *core.Metadata
	var flag int
	if group.super {
		msgid, prev_msgid, err := client.HandleSuperGroupMessage(msg, group)
		if err == nil {
			meta = &core.Metadata{SyncKey: msgid, PrevSyncKey: prev_msgid}
		}
		flag = core.MESSAGE_FLAG_SUPER_GROUP
	} else {
		msgid, prev_msgid, err := client.HandleGroupMessage(msg, group)
		if err == nil {
			meta = &core.Metadata{SyncKey: msgid, PrevSyncKey: prev_msgid}
		}
	}

	ack := &core.Message{Cmd: core.MSG_ACK, Flag: flag, Body: &core.MessageACK{Seq: int32(seq)}, Meta: meta}
	r := client.EnqueueMessage(ack)
	if !r {
		log.Warning("send group message ack error")
	}

	atomic.AddInt64(&server_summary.in_message_count, 1)
	log.Infof("group message sender:%d group id:%d super:%v", msg.Sender, msg.Receiver, group.super)
	if meta != nil {
		log.Info("group message ack meta:", meta.SyncKey, meta.PrevSyncKey)
	}
}

func (client *GroupClient) HandleGroupSync(group_sync_key *core.GroupSyncKey) {
	if client.uid == 0 {
		return
	}

	group_id := group_sync_key.GroupId

	group := group_manager.FindGroup(group_id)
	if group == nil {
		log.Warning("can't find group:", group_id)
		return
	}

	if !group.IsMember(client.uid) {
		log.Warningf("sender:%d is not group member", client.uid)
		return
	}

	ts := group.GetMemberTimestamp(client.uid)

	rpc := GetGroupStorageRPCClient(group_id)

	last_id := group_sync_key.SyncKey
	if last_id == 0 {
		last_id = GetGroupSyncKey(client.appid, client.uid, group_id)
	}

	s := &core.SyncGroupHistory{
		AppID:     client.appid,
		Uid:       client.uid,
		DeviceID:  client.device_ID,
		GroupID:   group_sync_key.GroupId,
		LastMsgID: last_id,
		Timestamp: int32(ts),
	}

	log.Info("sync group message...", group_sync_key.SyncKey, last_id)
	resp, err := rpc.Call("SyncGroupMessage", s)
	if err != nil {
		log.Warning("sync message err:", err)
		return
	}

	gh := resp.(*core.GroupHistoryMessage)
	messages := gh.Messages

	sk := &core.GroupSyncKey{SyncKey: last_id, GroupId: group_id}
	client.EnqueueMessage(&core.Message{Cmd: core.MSG_SYNC_GROUP_BEGIN, Body: sk})
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		log.Info("message:", msg.MsgID, core.Command(msg.Cmd))
		m := &core.Message{Cmd: int(msg.Cmd), Version: core.DEFAULT_VERSION}
		m.FromData(msg.Raw)
		sk.SyncKey = msg.MsgID
		if client.isSender(m, msg.DeviceID) {
			m.Flag |= core.MESSAGE_FLAG_SELF
		}
		client.EnqueueMessage(m)
	}

	if gh.LastMsgID < last_id && gh.LastMsgID > 0 {
		sk.SyncKey = gh.LastMsgID
		log.Warningf("group:%d client last id:%d server last id:%d", group_id, last_id, gh.LastMsgID)
	}
	client.EnqueueMessage(&core.Message{Cmd: core.MSG_SYNC_GROUP_END, Body: sk})
}

func (client *GroupClient) HandleGroupSyncKey(group_sync_key *core.GroupSyncKey) {
	if client.uid == 0 {
		return
	}

	group_id := group_sync_key.GroupId
	last_id := group_sync_key.SyncKey

	log.Info("group sync key:", group_sync_key.SyncKey, last_id)
	if last_id > 0 {
		s := &core.SyncGroupHistory{
			AppID:     client.appid,
			Uid:       client.uid,
			GroupID:   group_id,
			LastMsgID: last_id,
		}
		group_sync_c <- s
	}
}

func (client *GroupClient) HandleMessage(msg *core.Message) {
	switch msg.Cmd {
	case core.MSG_GROUP_IM:
		client.HandleGroupIMMessage(msg)
	case core.MSG_SYNC_GROUP:
		client.HandleGroupSync(msg.Body.(*core.GroupSyncKey))
	case core.MSG_GROUP_SYNC_KEY:
		client.HandleGroupSyncKey(msg.Body.(*core.GroupSyncKey))
	}
}
