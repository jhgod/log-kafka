/******************************************************
# DESC    : the udp server to get log by udp datagram
# AUTHOR  : Alex Stocks
# VERSION : 1.0
# LICENCE : Apache License 2.0
# EMAIL   : alexstocks@foxmail.com
# MOD     : 2018-03-22 20:46
# FILE    : udp_server.go
******************************************************/

package main

import (
	"fmt"
	"net"
	"sync"
	"time"
)

import (
	"github.com/AlexStocks/goext/time"
)

const (
	ReadDeadline = 5e9
)

var (
	protocol = "udp"
)

type (
	UdpServer struct {
		conn *net.UDPConn
		done chan empty
		wg   sync.WaitGroup
	}
)

func NewUdpServer() *UdpServer {
	var (
		err     error
		addr    string
		udpAddr *net.UDPAddr
		server  *UdpServer
	)
	//Build the address
	addr = fmt.Sprintf(":%d", Conf.Core.UDPPort)
	if udpAddr, err = net.ResolveUDPAddr(protocol, addr); err != nil {
		panic(fmt.Sprintf("Wrong Address %q", addr))
	}

	server = &UdpServer{done: make(chan empty)}
	//Create the connection
	if server.conn, err = net.ListenUDP(protocol, udpAddr); err != nil {
		panic(fmt.Sprintf("net.ListenUDP(protocol:%s, udpAddr:%#v) = error:%#v", protocol, udpAddr, err))
	}
	server.conn.SetReadBuffer(Conf.Core.UDPReadBufSize)

	server.wg.Add(1)
	go server.start()

	return server
}

func (u *UdpServer) start() {
	var (
		err      error
		ok       bool
		nerr     net.Error
		buf      []byte
		length   int
		seq      int
		peerAddr net.Addr
	)

	defer u.wg.Done()

	for {
		if u.isClosed() {
			Log.Warn("udp server exit now...")
			break
		}
		buf = make([]byte, Conf.Core.LogSize)
		u.conn.SetReadDeadline(time.Now().Add(gxtime.TimeSecondDuration(float64(Conf.Core.UDPReadTimeout))))
		length, peerAddr, err = u.conn.ReadFromUDP(buf)
		if nerr, ok = err.(net.Error); ok && nerr.Timeout() {
			continue
		}
		if length == 0 || err != nil {
			StatStorage.AddUdpError(1)
			Log.Warn("conn.ReadFromUDP() = {peer:%#v error:%#v}", peerAddr, err)
			continue
		}

		//fmt.Printf("udp seq:%d\n", seq)
		Worker.enqueueKafkaMessage(Message{
			topic: Conf.Kafka.DefaultTopic,
			key:   []byte(fmt.Sprintf("%d", seq)),
			value: buf[:length],
		})
		StatStorage.AddUdpSuccess(1)
		seq++
	}
}

// check whether the server has been closed.
func (u *UdpServer) isClosed() bool {
	select {
	case <-u.done:
		return true

	default:
		return false
	}
}

func (u *UdpServer) Stop() {
	close(u.done)
	u.wg.Wait()
	u.conn.Close()
}
