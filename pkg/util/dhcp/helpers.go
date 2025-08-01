// Copyright 2019 Yunion
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dhcp

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"

	"yunion.io/x/log"

	"yunion.io/x/onecloud/pkg/util/netutils2"
)

const (
	PXECLIENT = "PXEClient"

	OptClasslessRouteLin OptionCode = OptionClasslessRouteFormat //Classless Static Route Option
	OptClasslessRouteWin OptionCode = 249
)

// http://www.networksorcery.com/enp/rfc/rfc2132.txt
type ResponseConfig struct {
	InterfaceMac net.HardwareAddr
	VlanId       uint16

	OsName        string
	ServerIP      net.IP // OptServerIdentifier 54
	ClientIP      net.IP
	Gateway       net.IP                 // OptRouters 3
	Domain        string                 // OptDomainName 15
	LeaseTime     time.Duration          // OptLeaseTime 51
	RenewalTime   time.Duration          // OptRenewalTime 58
	BroadcastAddr net.IP                 // OptBroadcastAddr 28
	Hostname      string                 // OptHostname 12
	SubnetMask    net.IP                 // OptSubnetMask 1
	DNSServers    []net.IP               // OptDNSServers
	Routes        []netutils2.SRouteInfo // TODO: 249 for windows, 121 for linux
	NTPServers    []net.IP               // OptNTPServers 42
	MTU           uint16                 // OptMTU 26

	ClientIP6   net.IP
	Gateway6    net.IP
	PrefixLen6  uint8
	DNSServers6 []net.IP
	NTPServers6 []net.IP
	Routes6     []netutils2.SRouteInfo

	IsDefaultGW bool

	// Relay Info https://datatracker.ietf.org/doc/html/rfc3046
	RelayInfo []byte

	// TFTP config
	BootServer string
	BootFile   string
	BootBlock  uint16
}

func (conf ResponseConfig) GetHostname() string {
	return conf.Hostname
}

func GetOptUint16(val uint16) []byte {
	opts := []byte{0, 0}
	binary.BigEndian.PutUint16(opts, val)
	return opts
}

func GetOptUint32(val uint32) []byte {
	opts := []byte{0, 0, 0, 0}
	binary.BigEndian.PutUint32(opts, val)
	return opts
}

func GetOptIP(ip net.IP) []byte {
	return []byte(ip.To4())
}

func GetOptIPs(ips []net.IP) []byte {
	buf := make([]byte, 0)
	for _, ip := range ips {
		buf = append(buf, []byte(ip.To4())...)
	}
	return buf
}

func GetOptTime(d time.Duration) []byte {
	timeBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(timeBytes, uint32(d/time.Second))
	return timeBytes
}

func getClasslessRoutePack(route netutils2.SRouteInfo) []byte {
	// var snet, gw = route[0], route[1]
	// tmp := strings.Split(snet, "/")
	netaddr := route.Prefix
	if netaddr != nil {
		netaddr = netaddr.To4()
	}
	masklen := route.PrefixLen
	netlen := masklen / 8
	if masklen%8 > 0 {
		netlen += 1
	}
	if netlen < 4 {
		netaddr = netaddr[0:netlen]
	}
	gwaddr := route.Gateway
	if gwaddr != nil {
		gwaddr = gwaddr.To4()
	}
	res := []byte{byte(masklen)}
	res = append(res, []byte(netaddr)...)
	return append(res, []byte(gwaddr)...)
}

func MakeReplyPacket(pkt Packet, conf *ResponseConfig) (Packet, error) {
	msgType := Offer
	if pkt.Type() == Request {
		reqAddr, _ := pkt.ParseOptions().IP(OptionRequestedIPAddress)
		if reqAddr != nil && !conf.ClientIP.Equal(reqAddr) {
			msgType = NAK
		} else {
			msgType = ACK
		}
	}
	return makeDHCPReplyPacket(pkt, conf, msgType), nil
}

func getPacketVendorClassId(pkt Packet) string {
	bs := pkt.ParseOptions()[OptionVendorClassIdentifier]
	vendorClsId := string(bs)
	return vendorClsId
}

func makeDHCPReplyPacket(req Packet, conf *ResponseConfig, msgType MessageType) Packet {
	if conf.OsName == "" {
		if vendorClsId := getPacketVendorClassId(req); vendorClsId != "" && strings.HasPrefix(vendorClsId, "MSFT ") {
			conf.OsName = "win"
		}
	}

	opts := make([]Option, 0)

	if conf.SubnetMask != nil {
		opts = append(opts, Option{Code: OptionSubnetMask, Value: GetOptIP(conf.SubnetMask)})
	}
	if conf.Gateway != nil {
		opts = append(opts, Option{Code: OptionRouter, Value: GetOptIP(conf.Gateway)})
	}
	if conf.Domain != "" {
		opts = append(opts, Option{Code: OptionDomainName, Value: []byte(conf.Domain)})
	}
	if conf.BroadcastAddr != nil {
		opts = append(opts, Option{Code: OptionBroadcastAddress, Value: GetOptIP(conf.BroadcastAddr)})
	}
	if conf.Hostname != "" {
		opts = append(opts, Option{Code: OptionHostName, Value: []byte(conf.GetHostname())})
	}
	if len(conf.DNSServers) > 0 {
		opts = append(opts, Option{Code: OptionDomainNameServer, Value: GetOptIPs(conf.DNSServers)})
	}
	if len(conf.NTPServers) > 0 {
		opts = append(opts, Option{Code: OptionNetworkTimeProtocolServers, Value: GetOptIPs(conf.NTPServers)})
	}
	if conf.MTU > 0 {
		opts = append(opts, Option{Code: OptionInterfaceMTU, Value: GetOptUint16(conf.MTU)})
	}
	if conf.RelayInfo != nil {
		opts = append(opts, Option{Code: OptionRelayAgentInformation, Value: conf.RelayInfo})
	}
	var clientIP net.IP
	if conf.ClientIP != nil {
		clientIP = conf.ClientIP
	} else {
		clientIP = net.ParseIP("0.0.0.0")
		opts = append(opts, Option{Code: OptionIPv6Only, Value: GetOptUint32(60)})
	}
	resp := ReplyPacket(req, msgType, conf.ServerIP, clientIP, conf.LeaseTime, opts)
	if conf.BootServer != "" {
		//resp.Options[OptOverload] = []byte{3}
		resp.SetSIAddr(net.ParseIP(conf.BootServer))
		resp.AddOption(OptionTFTPServerName, []byte(fmt.Sprintf("%s\x00", conf.BootServer)))
	}
	if conf.BootFile != "" {
		resp.AddOption(OptionBootFileName, []byte(fmt.Sprintf("%s\x00", conf.BootFile)))
		sz := make([]byte, 2)
		binary.BigEndian.PutUint16(sz, conf.BootBlock)
		resp.AddOption(OptionBootFileSize, sz)
	}
	//if bs, _ := req.ParseOptions().Bytes(OptionClientMachineIdentifier); bs != nil {
	//resp.AddOption(OptionClientMachineIdentifier, bs)
	//}
	if conf.RenewalTime > 0 {
		resp.AddOption(OptionRenewalTimeValue, GetOptTime(conf.RenewalTime))
	}
	if conf.Routes != nil {
		var optCode = OptClasslessRouteLin
		if strings.HasPrefix(strings.ToLower(conf.OsName), "win") {
			optCode = OptClasslessRouteWin
		}
		for _, route := range conf.Routes {
			routeBytes := getClasslessRoutePack(route)
			resp.AddOption(optCode, routeBytes)
		}
	}
	return resp
}

func IsPXERequest(pkt Packet) bool {
	//if pkt.Type != MsgDiscover {
	//log.Warningf("packet is %s, not %s", pkt.Type, MsgDiscover)
	//return false
	//}

	if pkt.GetOptionValue(OptionClientArchitecture) == nil {
		log.Debugf("%s not a PXE boot request (missing option 93)", pkt.CHAddr().String())
		return false
	}
	return true
}
