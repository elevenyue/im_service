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
	log "github.com/golang/glog"
)
import "sync/atomic"

type RoomClient struct {
	*Connection
	room_id int64
}

func (client *RoomClient) Logout() {
	if client.room_id > 0 {
		channel := GetRoomChannel(client.room_id)
		channel.UnsubscribeRoom(client.appid, client.room_id)
		route := app_route.FindOrAddRoute(client.appid, func(appid int64) *Route {
			var router = NewRoute(appid)
			return router
		})
		route.RemoveRoomClient(client.room_id, client.Client())
	}
}

func (client *RoomClient) HandleMessage(msg *core.Message) {
	switch msg.Cmd {
	case core.MSG_ENTER_ROOM:
		client.HandleEnterRoom(msg.Body.(*core.Room))
	case core.MSG_LEAVE_ROOM:
		client.HandleLeaveRoom(msg.Body.(*core.Room))
	case core.MSG_ROOM_IM:
		client.HandleRoomIM(msg.Body.(*core.RoomMessage), msg.Seq)
	}
}

func (client *RoomClient) HandleEnterRoom(room *core.Room) {
	if client.uid == 0 {
		log.Warning("client has't been authenticated")
		return
	}

	room_id := room.RoomID()
	log.Info("enter room id:", room_id)
	if room_id == 0 || client.room_id == room_id {
		return
	}
	route := app_route.FindOrAddRoute(client.appid, func(appid int64) *Route {
		var router = NewRoute(appid)
		return router
	})
	if client.room_id > 0 {
		channel := GetRoomChannel(client.room_id)
		channel.UnsubscribeRoom(client.appid, client.room_id)

		route.RemoveRoomClient(client.room_id, client.Client())
	}

	client.room_id = room_id
	route.AddRoomClient(client.room_id, client.Client())
	channel := GetRoomChannel(client.room_id)
	channel.SubscribeRoom(client.appid, client.room_id)
}

func (client *RoomClient) HandleLeaveRoom(room *core.Room) {
	if client.uid == 0 {
		log.Warning("client has't been authenticated")
		return
	}

	room_id := room.RoomID()
	log.Info("leave room id:", room_id)
	if room_id == 0 {
		return
	}
	if client.room_id != room_id {
		return
	}

	route := app_route.FindOrAddRoute(client.appid, func(appid int64) *Route {
		var router = NewRoute(appid)
		return router
	})
	route.RemoveRoomClient(client.room_id, client.Client())
	channel := GetRoomChannel(client.room_id)
	channel.UnsubscribeRoom(client.appid, client.room_id)
	client.room_id = 0
}

func (client *RoomClient) HandleRoomIM(room_im *core.RoomMessage, seq int) {
	if client.uid == 0 {
		log.Warning("client has't been authenticated")
		return
	}
	room_id := room_im.Receiver
	if room_id != client.room_id {
		log.Warningf("room id:%d is't client's room id:%d\n", room_id, client.room_id)
		return
	}

	fb := atomic.LoadInt32(&client.forbidden)
	if fb == 1 {
		log.Infof("room id:%d client:%d, %d is forbidden", room_id, client.appid, client.uid)
		return
	}

	m := &core.Message{Cmd: core.MSG_ROOM_IM, Body: room_im}
	DispatchMessageToRoom(m, room_id, client.appid, client.Client())

	amsg := &core.AppMessage{Appid: client.appid, Receiver: room_id, Msg: m}
	channel := GetRoomChannel(client.room_id)
	channel.PublishRoom(amsg)

	client.wt <- &core.Message{Cmd: core.MSG_ACK, Body: &core.MessageACK{Seq: int32(seq)}}
}
