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
import log "github.com/golang/glog"

type CustomerClient struct {
	*Connection
}

func NewCustomerClient(conn *Connection) *CustomerClient {
	c := &CustomerClient{Connection: conn}
	return c
}

func (client *CustomerClient) HandleMessage(msg *core.Message) {
	switch msg.Cmd {
	case core.MSG_CUSTOMER:
		client.HandleCustomerMessage(msg)
	case core.MSG_CUSTOMER_SUPPORT:
		client.HandleCustomerSupportMessage(msg)
	}
}

//客服->顾客
func (client *CustomerClient) HandleCustomerSupportMessage(msg *core.Message) {
	cm := msg.Body.(*core.CustomerMessage)
	if client.appid != config.kefu_appid {
		log.Warningf("client appid:%d kefu appid:%d",
			client.appid, config.kefu_appid)
		return
	}
	if client.uid != cm.SellerId {
		log.Warningf("uid:%d seller id:%d", client.uid, cm.SellerId)
		return
	}

	cm.Timestamp = int32(time.Now().Unix())

	if (msg.Flag & core.MESSAGE_FLAG_UNPERSISTENT) > 0 {
		log.Info("customer support message unpersistent")
		SendAppMessage(cm.CustomerAppid, cm.CustomerId, msg)
		ack := &core.Message{Cmd: core.MSG_ACK, Body: &core.MessageACK{Seq: int32(msg.Seq)}}
		client.EnqueueMessage(ack)
		return
	}

	msgid, prev_msgid, err := SaveMessage(cm.CustomerAppid, cm.CustomerId, client.device_ID, msg)
	if err != nil {
		log.Warning("save customer support message err:", err)
		return
	}

	msgid2, prev_msgid2, err := SaveMessage(client.appid, cm.SellerId, client.device_ID, msg)
	if err != nil {
		log.Warning("save customer support message err:", err)
		return
	}

	PushMessage(cm.CustomerAppid, cm.CustomerId, msg)

	meta := &core.Metadata{SyncKey: msgid, PrevSyncKey: prev_msgid}
	m1 := &core.Message{Cmd: core.MSG_CUSTOMER_SUPPORT, Version: core.DEFAULT_VERSION, Flag: msg.Flag | core.MESSAGE_FLAG_PUSH, Body: msg.Body, Meta: meta}
	SendAppMessage(cm.CustomerAppid, cm.CustomerId, m1)

	notify := &core.Message{Cmd: core.MSG_SYNC_NOTIFY, Body: &core.SyncKey{msgid}}
	SendAppMessage(cm.CustomerAppid, cm.CustomerId, notify)

	//发送给自己的其它登录点
	meta = &core.Metadata{SyncKey: msgid2, PrevSyncKey: prev_msgid2}
	m2 := &core.Message{Cmd: core.MSG_CUSTOMER_SUPPORT, Version: core.DEFAULT_VERSION, Flag: msg.Flag | core.MESSAGE_FLAG_PUSH, Body: msg.Body, Meta: meta}
	client.SendMessage(client.uid, m2)

	notify = &core.Message{Cmd: core.MSG_SYNC_NOTIFY, Body: &core.SyncKey{msgid2}}
	client.SendMessage(client.uid, notify)

	meta = &core.Metadata{SyncKey: msgid2, PrevSyncKey: prev_msgid2}
	ack := &core.Message{Cmd: core.MSG_ACK, Body: &core.MessageACK{Seq: int32(msg.Seq)}, Meta: meta}
	client.EnqueueMessage(ack)
}

//顾客->客服
func (client *CustomerClient) HandleCustomerMessage(msg *core.Message) {
	cm := msg.Body.(*core.CustomerMessage)
	cm.Timestamp = int32(time.Now().Unix())

	log.Infof("customer message customer appid:%d customer id:%d store id:%d seller id:%d",
		cm.CustomerAppid, cm.CustomerId, cm.StoreId, cm.SellerId)
	if cm.CustomerAppid != client.appid {
		log.Warningf("message appid:%d client appid:%d",
			cm.CustomerAppid, client.appid)
		return
	}
	if cm.CustomerId != client.uid {
		log.Warningf("message customer id:%d client uid:%d",
			cm.CustomerId, client.uid)
		return
	}

	if cm.SellerId == 0 {
		log.Warningf("message seller id:0")
		return
	}

	if (msg.Flag & core.MESSAGE_FLAG_UNPERSISTENT) > 0 {
		log.Info("customer message unpersistent")
		SendAppMessage(config.kefu_appid, cm.SellerId, msg)
		ack := &core.Message{Cmd: core.MSG_ACK, Body: &core.MessageACK{Seq: int32(msg.Seq)}}
		client.EnqueueMessage(ack)
		return
	}

	msgid, prev_msgid, err := SaveMessage(config.kefu_appid, cm.SellerId, client.device_ID, msg)
	if err != nil {
		log.Warning("save customer message err:", err)
		return
	}

	msgid2, prev_msgid2, err := SaveMessage(cm.CustomerAppid, cm.CustomerId, client.device_ID, msg)
	if err != nil {
		log.Warning("save customer message err:", err)
		return
	}

	PushMessage(config.kefu_appid, cm.SellerId, msg)

	meta := &core.Metadata{SyncKey: msgid, PrevSyncKey: prev_msgid}
	m1 := &core.Message{Cmd: core.MSG_CUSTOMER, Version: core.DEFAULT_VERSION, Flag: msg.Flag | core.MESSAGE_FLAG_PUSH, Body: msg.Body, Meta: meta}
	SendAppMessage(config.kefu_appid, cm.SellerId, m1)

	notify := &core.Message{Cmd: core.MSG_SYNC_NOTIFY, Body: &core.SyncKey{msgid}}
	SendAppMessage(config.kefu_appid, cm.SellerId, notify)

	//发送给自己的其它登录点
	meta = &core.Metadata{SyncKey: msgid2, PrevSyncKey: prev_msgid2}
	m2 := &core.Message{Cmd: core.MSG_CUSTOMER, Version: core.DEFAULT_VERSION, Flag: msg.Flag | core.MESSAGE_FLAG_PUSH, Body: msg.Body, Meta: meta}
	client.SendMessage(client.uid, m2)

	notify = &core.Message{Cmd: core.MSG_SYNC_NOTIFY, Body: &core.SyncKey{msgid2}}
	client.SendMessage(client.uid, notify)

	meta = &core.Metadata{SyncKey: msgid2, PrevSyncKey: prev_msgid2}
	ack := &core.Message{Cmd: core.MSG_ACK, Body: &core.MessageACK{Seq: int32(msg.Seq)}, Meta: meta}
	client.EnqueueMessage(ack)
}
