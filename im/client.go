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
	"net"
)
import "time"
import "sync/atomic"
import log "github.com/golang/glog"
import "container/list"
import "crypto/tls"
import "fmt"

type Client struct {
	Connection //必须放在结构体首部
	*PeerClient
	*GroupClient
	*RoomClient
	*CustomerClient
	public_ip int32
}

func NewClient(conn interface{}) *Client {
	client := new(Client)

	//初始化Connection
	client.conn = conn // conn is net.Conn or engineio.Conn

	if net_conn, ok := conn.(net.Conn); ok {
		addr := net_conn.LocalAddr()
		if taddr, ok := addr.(*net.TCPAddr); ok {
			ip4 := taddr.IP.To4()
			client.public_ip = int32(ip4[0])<<24 | int32(ip4[1])<<16 | int32(ip4[2])<<8 | int32(ip4[3])
		}
	}

	client.wt = make(chan *core.Message, 300)
	client.lwt = make(chan int, 1) //only need 1
	//'10'对于用户拥有非常多的超级群，读线程还是有可能会阻塞
	client.pwt = make(chan []*core.Message, 10)
	client.messages = list.New()

	atomic.AddInt64(&server_summary.nconnections, 1)

	client.PeerClient = &PeerClient{&client.Connection}
	client.GroupClient = &GroupClient{&client.Connection}
	client.RoomClient = &RoomClient{Connection: &client.Connection}
	client.CustomerClient = NewCustomerClient(&client.Connection)
	return client
}

func handle_client(conn net.Conn) {
	log.Infoln("handle new connection, remote address:", conn.RemoteAddr())
	client := NewClient(conn)
	client.Run()
}

func handle_ssl_client(conn net.Conn) {
	log.Infoln("handle new ssl connection,  remote address:", conn.RemoteAddr())
	client := NewClient(conn)
	client.Run()
}

func Listen(f func(net.Conn), port int) {
	listen_addr := fmt.Sprintf("0.0.0.0:%d", port)
	listen, err := net.Listen("tcp", listen_addr)
	if err != nil {
		log.Errorf("listen err:%s", err)
		return
	}
	tcp_listener, ok := listen.(*net.TCPListener)
	if !ok {
		log.Error("listen err")
		return
	}

	for {
		client, err := tcp_listener.AcceptTCP()
		if err != nil {
			log.Errorf("accept err:%s", err)
			return
		}
		f(client)
	}
}

func ListenClient() {
	Listen(handle_client, config.port)
}

func ListenSSL(port int, cert_file, key_file string) {
	cert, err := tls.LoadX509KeyPair(cert_file, key_file)
	if err != nil {
		log.Fatal("load cert err:", err)
		return
	}
	config := &tls.Config{Certificates: []tls.Certificate{cert}}
	addr := fmt.Sprintf(":%d", port)
	listen, err := tls.Listen("tcp", addr, config)
	if err != nil {
		log.Fatal("ssl listen err:", err)
	}

	log.Infof("ssl listen...")
	for {
		conn, err := listen.Accept()
		if err != nil {
			log.Fatal("ssl accept err:", err)
		}
		handle_ssl_client(conn)
	}
}

func (client *Client) Read() {
	for {
		tc := atomic.LoadInt32(&client.tc)
		if tc > 0 {
			log.Infof("quit read goroutine, client:%d write goroutine blocked", client.uid)
			client.HandleClientClosed()
			break
		}

		t1 := time.Now().Unix()
		msg := client.read()
		t2 := time.Now().Unix()
		if t2-t1 > 6*60 {
			log.Infof("client:%d socket read timeout:%d %d", client.uid, t1, t2)
		}
		if msg == nil {
			client.HandleClientClosed()
			break
		}

		client.HandleMessage(msg)
		t3 := time.Now().Unix()
		if t3-t2 > 2 {
			log.Infof("client:%d handle message is too slow:%d %d", client.uid, t2, t3)
		}
	}
}

func (client *Client) RemoveClient() {
	route := app_route.FindRoute(client.appid)
	if route == nil {
		log.Warning("can't find app route")
		return
	}
	route.RemoveClient(client)

	if client.room_id > 0 {
		route.RemoveRoomClient(client.room_id, client)
	}
}

func (client *Client) HandleClientClosed() {
	atomic.AddInt64(&server_summary.nconnections, -1)
	if client.uid > 0 {
		atomic.AddInt64(&server_summary.nclients, -1)
	}
	atomic.StoreInt32(&client.closed, 1)

	client.RemoveClient()

	//quit when write goroutine received
	client.wt <- nil

	client.RoomClient.Logout()
	client.PeerClient.Logout()
}

func (client *Client) HandleMessage(msg *core.Message) {
	log.Info("msg Cmd:", core.Command(msg.Cmd))
	switch msg.Cmd {
	case core.MSG_AUTH_TOKEN:
		client.HandleAuthToken(msg.Body.(*core.AuthenticationToken), msg.Version)
	case core.MSG_ACK:
		client.HandleACK(msg.Body.(*core.MessageACK))
	case core.MSG_PING:
		client.HandlePing()
	}

	client.PeerClient.HandleMessage(msg)
	client.GroupClient.HandleMessage(msg)
	client.RoomClient.HandleMessage(msg)
	client.CustomerClient.HandleMessage(msg)
}

func (client *Client) AuthToken(token string) (int64, int64, int, bool, error) {
	appid, uid, err := LoadUserAccessToken(token)

	if err != nil {
		return 0, 0, 0, false, err
	}

	forbidden, notification_on, err := GetUserPreferences(appid, uid)
	if err != nil {
		return 0, 0, 0, false, err
	}

	return appid, uid, forbidden, notification_on, nil
}

func (client *Client) HandleAuthToken(login *core.AuthenticationToken, version int) {
	if client.uid > 0 {
		log.Info("repeat login")
		return
	}

	var err error
	appid, uid, fb, on, err := client.AuthToken(login.Token)
	if err != nil {
		log.Infof("auth token:%s err:%s", login.Token, err)
		msg := &core.Message{Cmd: core.MSG_AUTH_STATUS, Version: version, Body: &core.AuthenticationStatus{1}}
		client.EnqueueMessage(msg)
		return
	}
	if uid == 0 {
		log.Info("auth token uid==0")
		msg := &core.Message{Cmd: core.MSG_AUTH_STATUS, Version: version, Body: &core.AuthenticationStatus{1}}
		client.EnqueueMessage(msg)
		return
	}

	if login.PlatformId != core.PLATFORM_WEB && len(login.DeviceId) > 0 {
		client.device_ID, err = GetDeviceID(login.DeviceId, int(login.PlatformId))
		if err != nil {
			log.Info("auth token uid==0")
			msg := &core.Message{Cmd: core.MSG_AUTH_STATUS, Version: version, Body: &core.AuthenticationStatus{1}}
			client.EnqueueMessage(msg)
			return
		}
	}

	is_mobile := login.PlatformId == core.PLATFORM_IOS || login.PlatformId == core.PLATFORM_ANDROID
	online := true
	if on && !is_mobile {
		online = false
	}

	client.appid = appid
	client.uid = uid
	client.forbidden = int32(fb)
	client.notification_on = on
	client.online = online
	client.version = version
	client.device_id = login.DeviceId
	client.platform_id = login.PlatformId
	client.tm = time.Now()
	log.Infof("auth token:%s appid:%d uid:%d device id:%s:%d forbidden:%d notification on:%t online:%t",
		login.Token, client.appid, client.uid, client.device_id,
		client.device_ID, client.forbidden, client.notification_on, client.online)

	msg := &core.Message{Cmd: core.MSG_AUTH_STATUS, Version: version, Body: &core.AuthenticationStatus{0}}
	client.EnqueueMessage(msg)

	client.AddClient()

	client.PeerClient.Login()

	CountDAU(client.appid, client.uid)
	atomic.AddInt64(&server_summary.nclients, 1)
}

func (client *Client) AddClient() {
	route := app_route.FindOrAddRoute(client.appid, func(appid int64) *Route {
		var router = NewRoute(appid)
		return router
	})
	route.AddClient(client)
}

func (client *Client) HandlePing() {
	m := &core.Message{Cmd: core.MSG_PONG}
	client.EnqueueMessage(m)
	if client.uid == 0 {
		log.Warning("client has't been authenticated")
		return
	}
}

func (client *Client) HandleACK(ack *core.MessageACK) {
	log.Info("ack:", ack.Seq)
}

//发送等待队列中的消息
func (client *Client) SendMessages(seq int) int {
	var messages *list.List
	client.mutex.Lock()
	if client.messages.Len() == 0 {
		client.mutex.Unlock()
		return seq
	}
	messages = client.messages
	client.messages = list.New()
	client.mutex.Unlock()

	e := messages.Front()
	for e != nil {
		msg := e.Value.(*core.Message)
		if msg.Cmd == core.MSG_RT || msg.Cmd == core.MSG_IM || msg.Cmd == core.MSG_GROUP_IM {
			atomic.AddInt64(&server_summary.out_message_count, 1)
		}

		if msg.Meta != nil {
			seq++
			meta_msg := &core.Message{Cmd: core.MSG_METADATA, Seq: seq, Version: client.version, Body: msg.Meta}
			client.send(meta_msg)
		}
		seq++
		//以当前客户端所用版本号发送消息
		vmsg := &core.Message{Cmd: msg.Cmd, Seq: seq, Version: client.version, Flag: msg.Flag, Body: msg.Body}
		client.send(vmsg)

		e = e.Next()
	}
	return seq
}

func (client *Client) Write() {
	seq := 0
	running := true

	//发送在线消息
	for running {
		select {
		case msg := <-client.wt:
			if msg == nil {
				client.close()
				running = false
				log.Infof("client:%d socket closed", client.uid)
				break
			}
			if msg.Cmd == core.MSG_RT || msg.Cmd == core.MSG_IM || msg.Cmd == core.MSG_GROUP_IM {
				atomic.AddInt64(&server_summary.out_message_count, 1)
			}

			if msg.Meta != nil {
				seq++
				meta_msg := &core.Message{Cmd: core.MSG_METADATA, Seq: seq, Version: client.version, Body: msg.Meta}
				client.send(meta_msg)
			}

			seq++
			//以当前客户端所用版本号发送消息
			vmsg := &core.Message{Cmd: msg.Cmd, Seq: seq, Version: client.version, Flag: msg.Flag, Body: msg.Body}
			client.send(vmsg)
		case messages := <-client.pwt:
			for _, msg := range messages {
				if msg.Cmd == core.MSG_RT || msg.Cmd == core.MSG_IM || msg.Cmd == core.MSG_GROUP_IM {
					atomic.AddInt64(&server_summary.out_message_count, 1)
				}

				if msg.Meta != nil {
					seq++
					meta_msg := &core.Message{Cmd: core.MSG_METADATA, Seq: seq, Version: client.version, Body: msg.Meta}
					client.send(meta_msg)
				}
				seq++
				//以当前客户端所用版本号发送消息
				vmsg := &core.Message{Cmd: msg.Cmd, Seq: seq, Version: client.version, Flag: msg.Flag, Body: msg.Body}
				client.send(vmsg)
			}
		case <-client.lwt:
			seq = client.SendMessages(seq)
			break

		}
	}

	//等待200ms,避免发送者阻塞
	t := time.After(200 * time.Millisecond)
	running = true
	for running {
		select {
		case <-t:
			running = false
		case <-client.wt:
			log.Warning("msg is dropped")
		}
	}

	log.Info("write goroutine exit")
}

func (client *Client) Run() {
	go client.Write()
	go client.Read()
}
