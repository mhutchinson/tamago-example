// https://github.com/f-secure-foundry/tamago-example
//
// Copyright (c) F-Secure Corporation
// https://foundry.f-secure.com
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	usbnet "github.com/f-secure-foundry/imx-usbnet"
	"github.com/f-secure-foundry/tamago/soc/imx6/usb"

	"github.com/miekg/dns"
)

const (
	deviceIP  = "10.0.0.1"
	deviceMAC = "1a:55:89:a2:69:41"
	hostMAC   = "1a:55:89:a2:69:42"
	resolver  = "8.8.8.8:53"
)

var iface *usbnet.Interface

func startNetworking() {
	var err error

	iface, err = usbnet.Init(deviceIP, deviceMAC, hostMAC, 1)

	if err != nil {
		log.Fatalf("could not initialize USB networking, %v", err)
	}

	iface.EnableICMP()

	listenerSSH, err := iface.ListenerTCP4(22)

	if err != nil {
		log.Fatalf("could not initialize SSH listener, %v", err)
	}

	listenerHTTP, err := iface.ListenerTCP4(80)

	if err != nil {
		log.Fatalf("could not initialize HTTP listener, %v", err)
	}

	listenerHTTPS, err := iface.ListenerTCP4(443)

	if err != nil {
		log.Fatalf("could not initialize HTTP listener, %v", err)
	}

	// create index.html
	setupStaticWebAssets()

	go func() {
		// see ssh_server.go
		startSSHServer(listenerSSH, deviceIP, 22)
	}()

	go func() {
		// see web_server.go
		startWebServer(listenerHTTP, deviceIP, 80, false)
	}()

	go func() {
		// see web_server.go
		startWebServer(listenerHTTPS, deviceIP, 443, true)
	}()

	usb.USB1.Init()
	usb.USB1.DeviceMode()
	usb.USB1.Reset()

	// never returns
	usb.USB1.Start(iface.Device())
}

func resolve(s string) (r *dns.Msg, rtt time.Duration, err error) {
	if s[len(s)-1:] != "." {
		s += "."
	}

	msg := new(dns.Msg)
	msg.Id = dns.Id()
	msg.RecursionDesired = true

	msg.Question = make([]dns.Question, 1)
	msg.Question[0] = dns.Question{s, dns.TypeA, dns.ClassINET}

	conn := new(dns.Conn)

	if conn.Conn, err = iface.DialTCP4(resolver); err != nil {
		return
	}

	c := new(dns.Client)

	return c.ExchangeWithConn(msg, conn)
}

func getHttpClient() *http.Client {
	netTransport := &http.Transport{
		Dial: func(network, add string) (net.Conn, error) {
			log.Printf("Dialling %v %v", network, add)
			parts := strings.Split(add, ":")
			if len(parts) != 2 {
				// Dial is only called with the host:port (no scheme, no path)
				return nil, fmt.Errorf("expected host:port but got %q", add)
			}
			host, port := parts[0], parts[1]
			// Look up the hostname
			r, _, err := resolve(host)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve host: %v", err)
			}
			if len(r.Answer) == 0 {
				return nil, fmt.Errorf("failed to resolve A records for host %q", host)
			}
			var ip net.IP
			if a, ok := r.Answer[0].(*dns.A); ok {
				ip = a.A
			} else {
				return nil, fmt.Errorf("expected A record but got %T %q", r.Answer[0], r.Answer[0])
			}
			target := fmt.Sprintf("%s:%s", ip, port)
			log.Printf("Dialling add %q (%s)", add, target)
			return iface.DialTCP4(target)
		},
	}
	c := http.Client{
		Transport: netTransport,
	}
	return &c
}
