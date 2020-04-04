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
import "github.com/valyala/gorpc"
import log "github.com/golang/glog"

//个人消息／普通群消息／客服消息
func GetStorageRPCClient(uid int64) *gorpc.DispatcherClient {
	if uid < 0 {
		uid = -uid
	}
	index := uid % int64(len(rpc_clients))
	return rpc_clients[index]
}

func GetStorageRPCIndex(uid int64) int64 {
	if uid < 0 {
		uid = -uid
	}
	index := uid % int64(len(rpc_clients))
	return index
}

//超级群消息
func GetGroupStorageRPCClient(group_id int64) *gorpc.DispatcherClient {
	if group_id < 0 {
		group_id = -group_id
	}
	index := group_id % int64(len(group_rpc_clients))
	return group_rpc_clients[index]
}

func GetChannel(uid int64) *Channel {
	if uid < 0 {
		uid = -uid
	}
	index := uid % int64(len(route_channels))
	return route_channels[index]
}

func GetGroupChannel(group_id int64) *Channel {
	if group_id < 0 {
		group_id = -group_id
	}
	index := group_id % int64(len(group_route_channels))
	return group_route_channels[index]
}

func GetRoomChannel(room_id int64) *Channel {
	if room_id < 0 {
		room_id = -room_id
	}
	index := room_id % int64(len(route_channels))
	return route_channels[index]
}

func GetGroupMessageDeliver(group_id int64) *GroupMessageDeliver {
	if group_id < 0 {
		group_id = -group_id
	}

	deliver_index := atomic.AddUint64(&current_deliver_index, 1)
	index := deliver_index % uint64(len(group_message_delivers))
	return group_message_delivers[index]
}

func SaveGroupMessage(appid int64, gid int64, device_id int64, msg *core.Message) (int64, int64, error) {
	dc := GetGroupStorageRPCClient(gid)

	gm := &core.GroupMessage{
		AppID:    appid,
		GroupID:  gid,
		DeviceID: device_id,
		Cmd:      int32(msg.Cmd),
		Raw:      msg.ToData(),
	}
	resp, err := dc.Call("SaveGroupMessage", gm)
	if err != nil {
		log.Warning("save group message err:", err)
		return 0, 0, err
	}
	r := resp.([2]int64)
	msgid := r[0]
	prev_msgid := r[1]
	log.Infof("save group message:%d %d %d\n", appid, gid, msgid)
	return msgid, prev_msgid, nil
}

func SavePeerGroupMessage(appid int64, members []int64, device_id int64, m *core.Message) ([]int64, error) {

	if len(members) == 0 {
		return nil, nil
	}

	dc := GetStorageRPCClient(members[0])

	pm := &core.PeerGroupMessage{
		AppID:    appid,
		Members:  members,
		DeviceID: device_id,
		Cmd:      int32(m.Cmd),
		Raw:      m.ToData(),
	}

	resp, err := dc.Call("SavePeerGroupMessage", pm)
	if err != nil {
		log.Error("save peer group message err:", err)
		return nil, err
	}

	r := resp.([]int64)
	log.Infof("save peer group message:%d %v %d %v\n", appid, members, device_id, r)
	return r, nil
}

func SaveMessage(appid int64, uid int64, device_id int64, m *core.Message) (int64, int64, error) {
	dc := GetStorageRPCClient(uid)

	pm := &core.PeerMessage{
		AppID:    appid,
		Uid:      uid,
		DeviceID: device_id,
		Cmd:      int32(m.Cmd),
		Raw:      m.ToData(),
	}

	resp, err := dc.Call("SavePeerMessage", pm)
	if err != nil {
		log.Error("save peer message err:", err)
		return 0, 0, err
	}

	r := resp.([2]int64)
	msgid := r[0]
	prev_msgid := r[1]
	log.Infof("save peer message:%d %d %d %d\n", appid, uid, device_id, msgid)
	return msgid, prev_msgid, nil
}

//群消息通知(apns, gcm...)
func PushGroupMessage(appid int64, group *Group, m *core.Message) {
	channels := make(map[*Channel][]int64)
	members := group.Members()
	for member := range members {
		//不对自身推送
		if im, ok := m.Body.(*core.IMMessage); ok {
			if im.Sender == member {
				continue
			}
		}
		channel := GetChannel(member)
		if _, ok := channels[channel]; !ok {
			channels[channel] = []int64{member}
		} else {
			receivers := channels[channel]
			receivers = append(receivers, member)
			channels[channel] = receivers
		}
	}

	for channel, receivers := range channels {
		channel.Push(appid, receivers, m)
	}
}

//离线消息推送
func PushMessage(appid int64, uid int64, m *core.Message) {
	channel := GetChannel(uid)
	channel.Push(appid, []int64{uid}, m)
}

func PublishMessage(appid int64, uid int64, msg *core.Message) {
	now := time.Now().UnixNano()
	amsg := &core.AppMessage{Appid: appid, Receiver: uid, Timestamp: now, Msg: msg}
	if msg.Meta != nil {
		amsg.Msgid = msg.Meta.SyncKey
		amsg.PrevMsgid = msg.Meta.PrevSyncKey
	}
	channel := GetChannel(uid)
	channel.Publish(amsg)
}

func PublishGroupMessage(appid int64, group_id int64, msg *core.Message) {
	now := time.Now().UnixNano()
	amsg := &core.AppMessage{Appid: appid, Receiver: group_id, Timestamp: now, Msg: msg}
	if msg.Meta != nil {
		amsg.Msgid = msg.Meta.SyncKey
		amsg.PrevMsgid = msg.Meta.PrevSyncKey
	}
	channel := GetGroupChannel(group_id)
	channel.PublishGroup(amsg)
}

func SendAppGroupMessage(appid int64, group *Group, msg *core.Message) {
	now := time.Now().UnixNano()
	amsg := &core.AppMessage{Appid: appid, Receiver: group.gid, Msgid: 0, Timestamp: now, Msg: msg}
	channel := GetGroupChannel(group.gid)
	channel.PublishGroup(amsg)
	DispatchMessageToGroup(msg, group, appid, nil)
}

func SendAppMessage(appid int64, uid int64, msg *core.Message) {
	now := time.Now().UnixNano()
	amsg := &core.AppMessage{Appid: appid, Receiver: uid, Msgid: 0, Timestamp: now, Msg: msg}
	channel := GetChannel(uid)
	channel.Publish(amsg)
	DispatchMessageToPeer(msg, uid, appid, nil)
}

func DispatchAppMessage(amsg *core.AppMessage) {
	now := time.Now().UnixNano()
	d := now - amsg.Timestamp
	log.Infof("dispatch app message:%s %d %d", core.Command(amsg.Msg.Cmd), amsg.Msg.Flag, d)
	if d > int64(time.Second) {
		log.Warning("dispatch app message slow...")
	}

	if amsg.Msgid > 0 {
		if (amsg.Msg.Flag & core.MESSAGE_FLAG_PUSH) == 0 {
			log.Fatal("invalid message flag", amsg.Msg.Flag)
		}
		meta := &core.Metadata{SyncKey: amsg.Msgid, PrevSyncKey: amsg.PrevMsgid}
		amsg.Msg.Meta = meta
	}
	DispatchMessageToPeer(amsg.Msg, amsg.Receiver, amsg.Appid, nil)
}

func DispatchRoomMessage(amsg *core.AppMessage) {
	log.Info("dispatch room message", core.Command(amsg.Msg.Cmd))

	room_id := amsg.Receiver
	DispatchMessageToRoom(amsg.Msg, room_id, amsg.Appid, nil)
}

func DispatchGroupMessage(amsg *core.AppMessage) {
	now := time.Now().UnixNano()
	d := now - amsg.Timestamp
	log.Infof("dispatch group message:%s %d %d", core.Command(amsg.Msg.Cmd), amsg.Msg.Flag, d)
	if d > int64(time.Second) {
		log.Warning("dispatch group message slow...")
	}

	if amsg.Msgid > 0 {
		if (amsg.Msg.Flag & core.MESSAGE_FLAG_PUSH) == 0 {
			log.Fatal("invalid message flag", amsg.Msg.Flag)
		}
		if (amsg.Msg.Flag & core.MESSAGE_FLAG_SUPER_GROUP) == 0 {
			log.Fatal("invalid message flag", amsg.Msg.Flag)
		}

		meta := &core.Metadata{SyncKey: amsg.Msgid, PrevSyncKey: amsg.PrevMsgid}
		amsg.Msg.Meta = meta
	}

	deliver := GetGroupMessageDeliver(amsg.Receiver)
	deliver.DispatchMessage(amsg)
}

func DispatchMessageToGroup(msg *core.Message, group *Group, appid int64, client *Client) bool {
	if group == nil {
		return false
	}

	route := app_route.FindRoute(appid)
	if route == nil {
		log.Warningf("can't dispatch app message, appid:%d uid:%d Cmd:%s", appid, group.gid, core.Command(msg.Cmd))
		return false
	}

	members := group.Members()
	for member := range members {
		clients := route.FindClientSet(member)
		if len(clients) == 0 {
			continue
		}

		for c, _ := range clients {
			if c == client {
				continue
			}
			c.EnqueueNonBlockMessage(msg)
		}
	}

	return true
}

func DispatchMessageToPeer(msg *core.Message, uid int64, appid int64, client *Client) bool {
	route := app_route.FindRoute(appid)
	if route == nil {
		log.Warningf("can't dispatch app message, appid:%d uid:%d Cmd:%s", appid, uid, core.Command(msg.Cmd))
		return false
	}
	clients := route.FindClientSet(uid)
	if len(clients) == 0 {
		return false
	}

	for c, _ := range clients {
		if c == client {
			continue
		}
		c.EnqueueNonBlockMessage(msg)
	}
	return true
}

func DispatchMessageToRoom(msg *core.Message, room_id int64, appid int64, client *Client) bool {
	route := app_route.FindOrAddRoute(appid, func(appid int64) *Route {
		var router = NewRoute(appid)
		return router
	})
	clients := route.FindRoomClientSet(room_id)

	if len(clients) == 0 {
		return false
	}
	for c, _ := range clients {
		if c == client {
			continue
		}
		c.EnqueueNonBlockMessage(msg)
	}
	return true
}
