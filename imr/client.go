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
	"net"
)
import log "github.com/golang/glog"

type Push struct {
	queue_name string
	content    []byte
}

type Client struct {
	wt chan *core.Message

	pwt chan *Push

	conn      *net.TCPConn
	app_route *AppRoute
}

func NewClient(conn *net.TCPConn) *Client {
	client := new(Client)
	client.conn = conn
	client.pwt = make(chan *Push, 10000)
	client.wt = make(chan *core.Message, 10)
	client.app_route = NewAppRoute()
	return client
}

func (client *Client) ContainAppUserID(id *core.AppUserID) bool {
	route := client.app_route.FindRoute(id.Appid)
	if route == nil {
		return false
	}
	return route.ContainUserID(id.Uid)
}

func (client *Client) IsAppUserOnline(id *core.AppUserID) bool {
	route := client.app_route.FindRoute(id.Appid)
	if route == nil {
		return false
	}

	return route.IsUserOnline(id.Uid)
}

func (client *Client) ContainAppRoomID(id *core.AppRoomID) bool {
	route := client.app_route.FindRoute(id.Appid)
	if route == nil {
		return false
	}

	return route.ContainRoomID(id.RoomId)
}

func (client *Client) Read() {
	AddClient(client)
	for {
		msg := client.read()
		if msg == nil {
			RemoveClient(client)
			client.pwt <- nil
			client.wt <- nil
			break
		}
		client.HandleMessage(msg)
	}
}

func (client *Client) HandleMessage(msg *core.Message) {
	log.Info("msg cmd:", core.Command(msg.Cmd))
	switch msg.Cmd {
	case core.MSG_SUBSCRIBE:
		client.HandleSubscribe(msg.Body.(*core.SubscribeMessage))
	case core.MSG_UNSUBSCRIBE:
		client.HandleUnsubscribe(msg.Body.(*core.AppUserID))
	case core.MSG_PUBLISH:
		client.HandlePublish(msg.Body.(*core.AppMessage))
	case core.MSG_PUBLISH_GROUP:
		client.HandlePublishGroup(msg.Body.(*core.AppMessage))
	case core.MSG_PUSH:
		client.HandlePush(msg.Body.(*core.BatchPushMessage))
	case core.MSG_SUBSCRIBE_ROOM:
		client.HandleSubscribeRoom(msg.Body.(*core.AppRoomID))
	case core.MSG_UNSUBSCRIBE_ROOM:
		client.HandleUnsubscribeRoom(msg.Body.(*core.AppRoomID))
	case core.MSG_PUBLISH_ROOM:
		client.HandlePublishRoom(msg.Body.(*core.AppMessage))
	default:
		log.Warning("unknown message cmd:", msg.Cmd)
	}
}

func (client *Client) HandleSubscribe(id *core.SubscribeMessage) {
	log.Infof("subscribe appid:%d uid:%d online:%d", id.Appid, id.Uid, id.Online)
	route := client.app_route.FindOrAddRoute(id.Appid, func(appid int64) *Route {
		return NewRoute(appid)
	})
	on := id.Online != 0
	route.AddUserID(id.Uid, on)
}

func (client *Client) HandleUnsubscribe(id *core.AppUserID) {
	log.Infof("unsubscribe appid:%d uid:%d", id.Appid, id.Uid)
	route := client.app_route.FindOrAddRoute(id.Appid, func(appid int64) *Route {
		return NewRoute(appid)
	})
	route.RemoveUserID(id.Uid)
}

func (client *Client) HandlePublishGroup(amsg *core.AppMessage) {
	log.Infof("publish message appid:%d group id:%d msgid:%d cmd:%s", amsg.Appid, amsg.Receiver, amsg.Msgid, core.Command(amsg.Msg.Cmd))

	//群发给所有接入服务器
	s := GetClientSet()

	msg := &core.Message{Cmd: core.MSG_PUBLISH_GROUP, Body: amsg}
	for c := range s {
		//不发送给自身
		if client == c {
			continue
		}
		c.wt <- msg
	}
}

func (client *Client) HandlePush(pmsg *core.BatchPushMessage) {
	log.Infof("push message appid:%d cmd:%s", pmsg.Appid, core.Command(pmsg.Msg.Cmd))

	off_members := make([]int64, 0)

	for _, uid := range pmsg.Receivers {
		if !IsUserOnline(pmsg.Appid, uid) {
			off_members = append(off_members, uid)
		}
	}

	cmd := pmsg.Msg.Cmd
	if len(off_members) > 0 {
		if cmd == core.MSG_GROUP_IM {
			client.PublishGroupMessage(pmsg.Appid, off_members, pmsg.Msg.Body.(*core.IMMessage))
		} else if cmd == core.MSG_IM {
			//assert len(off_members) == 1
			client.PublishPeerMessage(pmsg.Appid, pmsg.Msg.Body.(*core.IMMessage))
		} else if cmd == core.MSG_CUSTOMER ||
			cmd == core.MSG_CUSTOMER_SUPPORT {
			//assert len(off_members) == 1
			receiver := off_members[0]
			client.PublishCustomerMessage(pmsg.Appid, receiver,
				pmsg.Msg.Body.(*core.CustomerMessage), pmsg.Msg.Cmd)
		} else if cmd == core.MSG_SYSTEM {
			//assert len(off_members) == 1
			receiver := off_members[0]
			sys := pmsg.Msg.Body.(*core.SystemMessage)
			if config.is_push_system {
				client.PublishSystemMessage(pmsg.Appid, receiver, sys.Notification)
			}
		}
	}
}

func (client *Client) HandlePublish(amsg *core.AppMessage) {
	log.Infof("publish message appid:%d uid:%d msgid:%d cmd:%s", amsg.Appid, amsg.Receiver, amsg.Msgid, core.Command(amsg.Msg.Cmd))

	receiver := &core.AppUserID{Appid: amsg.Appid, Uid: amsg.Receiver}
	s := FindClientSet(receiver)
	msg := &core.Message{Cmd: core.MSG_PUBLISH, Body: amsg}
	for c := range s {
		//不发送给自身
		if client == c {
			continue
		}
		c.wt <- msg
	}
}

func (client *Client) HandleSubscribeRoom(id *core.AppRoomID) {
	log.Infof("subscribe appid:%d room id:%d", id.Appid, id.RoomId)
	route := client.app_route.FindOrAddRoute(id.Appid, func(appid int64) *Route {
		return NewRoute(appid)
	})
	route.AddRoomID(id.RoomId)
}

func (client *Client) HandleUnsubscribeRoom(id *core.AppRoomID) {
	log.Infof("unsubscribe appid:%d room id:%d", id.Appid, id.RoomId)
	route := client.app_route.FindOrAddRoute(id.Appid, func(appid int64) *Route {
		return NewRoute(appid)
	})
	route.RemoveRoomID(id.RoomId)
}

func (client *Client) HandlePublishRoom(amsg *core.AppMessage) {
	log.Infof("publish room message appid:%d room id:%d cmd:%s", amsg.Appid, amsg.Receiver, core.Command(amsg.Msg.Cmd))
	receiver := &core.AppRoomID{Appid: amsg.Appid, RoomId: amsg.Receiver}
	s := FindRoomClientSet(receiver)

	msg := &core.Message{Cmd: core.MSG_PUBLISH_ROOM, Body: amsg}
	for c := range s {
		//不发送给自身
		if client == c {
			continue
		}
		log.Info("publish room message")
		c.wt <- msg
	}
}

func (client *Client) Write() {
	seq := 0
	for {
		msg := <-client.wt
		if msg == nil {
			client.close()
			log.Infof("client socket closed")
			break
		}
		seq++
		msg.Seq = seq
		client.send(msg)
	}
}

func (client *Client) Run() {
	go client.Write()
	go client.Read()
	go client.Push()
}

func (client *Client) read() *core.Message {
	return core.ReceiveMessage(client.conn)
}

func (client *Client) send(msg *core.Message) {
	core.SendMessage(client.conn, msg)
}

func (client *Client) close() {
	client.conn.Close()
}
