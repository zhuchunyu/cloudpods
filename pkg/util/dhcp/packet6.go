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
	"net"
	"strings"

	"yunion.io/x/log"
	"yunion.io/x/pkg/errors"
)

// DHCPv6 https://datatracker.ietf.org/doc/html/rfc8415
/*
	0                   1                   2                   3
	0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|    msg-type   |               transaction-id                  |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	|                                                               |
	.                            options                            .
	.                 (variable number and length)                  .
	|                                                               |
	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
*/
/*
    0                   1                   2                   3
    0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |    msg-type   |   hop-count   |                               |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+                               |
   |                                                               |
   |                         link-address                          |
   |                                                               |
   |                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-|
   |                               |                               |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+                               |
   |                                                               |
   |                         peer-address                          |
   |                                                               |
   |                               +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-|
   |                               |                               |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+                               |
   .                                                               .
   .            options (variable number and length)   ....        .
   |                                                               |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
*/

// DHCPv6 Message Type
const (
	DHCPV6_SOLICIT             MessageType = 1
	DHCPV6_ADVERTISE           MessageType = 2
	DHCPV6_REQUEST             MessageType = 3
	DHCPV6_CONFIRM             MessageType = 4
	DHCPV6_RENEW               MessageType = 5
	DHCPV6_REBIND              MessageType = 6
	DHCPV6_REPLY               MessageType = 7
	DHCPV6_RELEASE             MessageType = 8
	DHCPV6_DECLINE             MessageType = 9
	DHCPV6_RECONFIGURE         MessageType = 10
	DHCPV6_INFORMATION_REQUEST MessageType = 11
	DHCPV6_RELAY_FORW          MessageType = 12
	DHCPV6_RELAY_REPL          MessageType = 13
)

type OptionCode6 uint16

const (
	DHCPV6_OPTION_CLIENTID                 OptionCode6 = 1
	DHCPV6_OPTION_SERVERID                 OptionCode6 = 2
	DHCPV6_OPTION_IA_NA                    OptionCode6 = 3
	DHCPV6_OPTION_IA_TA                    OptionCode6 = 4
	DHCPV6_OPTION_IAADDR                   OptionCode6 = 5
	DHCPV6_OPTION_ORO                      OptionCode6 = 6
	DHCPV6_OPTION_PREFERENCE               OptionCode6 = 7
	DHCPV6_OPTION_ELAPSED_TIME             OptionCode6 = 8
	DHCPV6_OPTION_RELAY_MSG                OptionCode6 = 9
	DHCPV6_OPTION_AUTH                     OptionCode6 = 11
	DHCPV6_OPTION_UNICAST                  OptionCode6 = 12
	DHCPV6_OPTION_STATUS_CODE              OptionCode6 = 13
	DHCPV6_OPTION_RAPID_COMMIT             OptionCode6 = 14
	DHCPV6_OPTION_USER_CLASS               OptionCode6 = 15
	DHCPV6_OPTION_VENDOR_CLASS             OptionCode6 = 16
	DHCPV6_OPTION_VENDOR_OPTS              OptionCode6 = 17
	DHCPV6_OPTION_INTERFACE_ID             OptionCode6 = 18
	DHCPV6_OPTION_RECONF_MSG               OptionCode6 = 19
	DHCPV6_OPTION_RECONF_ACCEPT            OptionCode6 = 20
	DHCPV6_OPTION_IA_PD                    OptionCode6 = 25
	DHCPV6_OPTION_IAPREFIX                 OptionCode6 = 26
	DHCPV6_OPTION_INFORMATION_REFRESH_TIME OptionCode6 = 32
	DHCPV6_OPTION_SOL_MAX_RT               OptionCode6 = 82
	DHCPV6_OPTION_INF_MAX_RT               OptionCode6 = 83

	// https://www.rfc-editor.org/rfc/rfc3646
	OPTION_DNS_SERVERS OptionCode6 = 23
	OPTION_DOMAIN_LIST OptionCode6 = 24
	// https://www.rfc-editor.org/rfc/rfc4075
	OPTION_SNTP_SERVERS OptionCode6 = 31
	//https://www.rfc-editor.org/rfc/rfc5908
	OPTION_NTP_SERVERS6 OptionCode6 = 56
	// https://www.rfc-editor.org/rfc/rfc4833
	OPTION_NEW_POSIX_TIMEZONE OptionCode6 = 41
	OPTION_NEW_TZDB_TIMEZONE  OptionCode6 = 42
)

func (opt OptionCode6) String() string {
	switch opt {
	case DHCPV6_OPTION_CLIENTID:
		return "DHCPV6_OPTION_CLIENTID"
	case DHCPV6_OPTION_SERVERID:
		return "DHCPV6_OPTION_SERVERID"
	case DHCPV6_OPTION_IA_NA:
		return "DHCPV6_OPTION_IA_NA"
	case DHCPV6_OPTION_IA_TA:
		return "DHCPV6_OPTION_IA_TA"
	case DHCPV6_OPTION_IAADDR:
		return "DHCPV6_OPTION_IAADDR"
	case DHCPV6_OPTION_ORO:
		return "DHCPV6_OPTION_ORO"
	case DHCPV6_OPTION_PREFERENCE:
		return "DHCPV6_OPTION_PREFERENCE"
	case DHCPV6_OPTION_ELAPSED_TIME:
		return "DHCPV6_OPTION_ELAPSED_TIME"
	case DHCPV6_OPTION_RELAY_MSG:
		return "DHCPV6_OPTION_RELAY_MSG"
	case DHCPV6_OPTION_AUTH:
		return "DHCPV6_OPTION_AUTH"
	case DHCPV6_OPTION_UNICAST:
		return "DHCPV6_OPTION_UNICAST"
	case DHCPV6_OPTION_STATUS_CODE:
		return "DHCPV6_OPTION_STATUS_CODE"
	case DHCPV6_OPTION_RAPID_COMMIT:
		return "DHCPV6_OPTION_RAPID_COMMIT"
	case DHCPV6_OPTION_USER_CLASS:
		return "DHCPV6_OPTION_USER_CLASS"
	case DHCPV6_OPTION_VENDOR_CLASS:
		return "DHCPV6_OPTION_VENDOR_CLASS"
	case DHCPV6_OPTION_VENDOR_OPTS:
		return "DHCPV6_OPTION_VENDOR_OPTS"
	case DHCPV6_OPTION_INTERFACE_ID:
		return "DHCPV6_OPTION_INTERFACE_ID"
	case DHCPV6_OPTION_RECONF_MSG:
		return "DHCPV6_OPTION_RECONF_MSG"
	case DHCPV6_OPTION_RECONF_ACCEPT:
		return "DHCPV6_OPTION_RECONF_ACCEPT"
	case DHCPV6_OPTION_IA_PD:
		return "DHCPV6_OPTION_IA_PD"
	case DHCPV6_OPTION_IAPREFIX:
		return "DHCPV6_OPTION_IAPREFIX"
	case DHCPV6_OPTION_INFORMATION_REFRESH_TIME:
		return "DHCPV6_OPTION_INFORMATION_REFRESH_TIME"
	case DHCPV6_OPTION_SOL_MAX_RT:
		return "DHCPV6_OPTION_SOL_MAX_RT"
	case DHCPV6_OPTION_INF_MAX_RT:
		return "DHCPV6_OPTION_INF_MAX_RT"
	case OPTION_DNS_SERVERS:
		return "OPTION_DNS_SERVERS"
	case OPTION_DOMAIN_LIST:
		return "OPTION_DOMAIN_LIST"
	case OPTION_SNTP_SERVERS:
		return "OPTION_SNTP_SERVERS"
	case OPTION_NTP_SERVERS6:
		return "OPTION_NTP_SERVERS6"
	case OPTION_NEW_POSIX_TIMEZONE:
		return "OPTION_NEW_POSIX_TIMEZONE"
	case OPTION_NEW_TZDB_TIMEZONE:
		return "OPTION_NEW_TZDB_TIMEZONE"
	}
	return "DHCPV6_OPTION_UNKNOWN"
}

// DHCPv6 message type
func (p Packet) Type6() MessageType {
	return MessageType(p[0])
}

// DHCPv6 transaction ID
func (p Packet) TID6() (uint32, error) {
	if !p.IsRelayMsg() {
		if len(p) < 4 {
			return 0, errors.Wrapf(errors.ErrInvalidFormat, "packet too short")
		}
		return binary.BigEndian.Uint32([]byte{0, p[1], p[2], p[3]}), nil
	}
	options := p.GetOption6s()
	for _, o := range options {
		if o.Code == DHCPV6_OPTION_RELAY_MSG {
			return Packet(o.Value).TID6()
		}
	}
	return 0, errors.Wrapf(errors.ErrInvalidFormat, "not a valid relay message")
}

func (p Packet) ClientID() ([]byte, error) {
	if !p.IsRelayMsg() {
		options := p.GetOption6s()
		for _, o := range options {
			if o.Code == DHCPV6_OPTION_CLIENTID {
				return o.Value, nil
			}
		}
		return nil, errors.Wrapf(errors.ErrInvalidFormat, "clientID option not found")
	}
	options := p.GetOption6s()
	for _, o := range options {
		if o.Code == DHCPV6_OPTION_RELAY_MSG {
			return Packet(o.Value).ClientID()
		}
	}
	return nil, errors.Wrapf(errors.ErrInvalidFormat, "not a valid relay message")
}

// DHCPv6 hop Count for relay message
func (p Packet) HopCount() byte {
	return p[1]
}

// DHCPv6 link address for relay message
func (p Packet) LinkAddr() net.IP {
	return net.IP(p[2:18])
}

// DHCPv6 peer address for relay message
func (p Packet) PeerAddr() net.IP {
	return net.IP(p[18:34])
}

func (p Packet) IsRelayMsg() bool {
	return p.Type6() == DHCPV6_RELAY_FORW || p.Type6() == DHCPV6_RELAY_REPL
}

func (p *Packet) SetType6(hType MessageType) {
	(*p)[0] = byte(hType)
}

func (p *Packet) SetTID(tid uint32) {
	tidBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(tidBytes, tid)
	(*p)[1] = tidBytes[1]
	(*p)[2] = tidBytes[2]
	(*p)[3] = tidBytes[3]
}

func (p *Packet) SetHopCount(hops byte) {
	(*p)[1] = hops
}

func (p *Packet) SetLinkAddr(linkAddr net.IP) {
	copy((*p)[2:18], linkAddr)
}

func (p *Packet) SetPeerAddr(peerAddr net.IP) {
	copy((*p)[18:34], peerAddr)
}

func NewPacket6(opCode MessageType, tid uint32) Packet {
	p := make(Packet, 4)
	p.SetType6(opCode)
	p.SetTID(tid)
	return p
}

func NewRelayPacket6() Packet {
	p := make(Packet, 34)
	p.SetType6(DHCPV6_RELAY_FORW)
	return p
}

type Option6 struct {
	Code  OptionCode6
	Value []byte
}

// Appends a DHCP option to the end of a packet
/*
    0                   1                   2                   3
    0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |          option-code          |           option-len          |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |                          option-data                          |
   |                      (option-len octets)                      |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
*/
func optionToBytes(o Option6) []byte {
	buf := make([]byte, 4+len(o.Value))
	binary.BigEndian.PutUint16(buf[0:2], uint16(o.Code))
	binary.BigEndian.PutUint16(buf[2:4], uint16(len(o.Value)))
	copy(buf[4:], o.Value)
	return buf
}

func optionsToBytes(opts []Option6) []byte {
	buf := make([]byte, 0)
	for i := range opts {
		buf = append(buf, optionToBytes(opts[i])...)
	}
	return buf
}

func (p Packet) GetOption6s() []Option6 {
	options := make([]Option6, 0)
	offset := 4
	if p.IsRelayMsg() {
		offset = 16*2 + 2
	}
	i := offset
	for i < len(p) {
		code := binary.BigEndian.Uint16(p[i : i+2])
		length := binary.BigEndian.Uint16(p[i+2 : i+4])
		value := make([]byte, length)
		copy(value, p[i+4:i+4+int(length)])
		options = append(options, Option6{
			Code:  OptionCode6(code),
			Value: value,
		})
		i += 4 + int(length)
	}
	return options
}

func MakeDHCP6Reply(pkt Packet, conf *ResponseConfig) (Packet, error) {
	var msgType MessageType
	pktType := pkt.Type6()
	switch pktType {
	case DHCPV6_SOLICIT:
		msgType = DHCPV6_ADVERTISE
	case DHCPV6_REQUEST:
		msgType = DHCPV6_REPLY
	case DHCPV6_CONFIRM:
		msgType = DHCPV6_REPLY
	case DHCPV6_RENEW:
		msgType = DHCPV6_REPLY
	case DHCPV6_REBIND:
		msgType = DHCPV6_REPLY
	case DHCPV6_INFORMATION_REQUEST:
		msgType = DHCPV6_REPLY
	default:
		return nil, errors.Wrapf(errors.ErrNotSupported, "unsupported message type %d", pktType)
	}

	return makeDHCPReplyPacket6(pkt, conf, msgType)
}

const (
	DUID_TYPE_LINK_LAYER_ADDRESS = 3
	DUID_HARDWARE_TYPE_ETHERNET  = 1
)

func makeServerId(serverMac net.HardwareAddr) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint16(buf[0:2], uint16(DUID_TYPE_LINK_LAYER_ADDRESS))
	binary.BigEndian.PutUint16(buf[2:4], uint16(DUID_HARDWARE_TYPE_ETHERNET))
	buf = append(buf, serverMac[:6]...)
	return buf
}

func makeIAAddr(ip net.IP, preferLT, validLT uint32, opts []Option6) []byte {
	buf := make([]byte, 24)
	copy(buf[0:16], ip.To16())
	binary.BigEndian.PutUint32(buf[16:20], preferLT)
	binary.BigEndian.PutUint32(buf[20:24], validLT)
	buf = append(buf, optionsToBytes(opts)...)
	return buf
}

func responseIANA(buf []byte, opts []Option6) []byte {
	// IA_NA structure: IAID (4 bytes) + T1 (4 bytes) + T2 (4 bytes)
	// Preserve the original IAID, T1, and T2 values from the client request
	if len(buf) < 12 {
		// If buffer is too short, pad with zeros
		padding := make([]byte, 12-len(buf))
		buf = append(buf, padding...)
	} else if len(buf) > 12 {
		// If buffer is too long, truncate to 12 bytes (IAID + T1 + T2)
		buf = buf[:12]
	}

	// Log the IA_NA parameters for debugging
	if len(buf) >= 12 {
		iaID := binary.BigEndian.Uint32(buf[0:4])
		t1 := binary.BigEndian.Uint32(buf[4:8])
		t2 := binary.BigEndian.Uint32(buf[8:12])
		log.Debugf("responseIANA IA_NA IAID %d t1 %d t2 %d", iaID, t1, t2)
	}

	buf = append(buf, optionsToBytes(opts)...)
	return buf
}

func makeIPv6s(ips []net.IP) []byte {
	buf := make([]byte, 0)
	for _, ip := range ips {
		ip6 := ip.To16()
		if ip6 == nil {
			continue
		}
		buf = append(buf, ip6...)
	}
	return buf
}

func decodeRequestOptions(value []byte) []OptionCode6 {
	var optCodes []OptionCode6
	for i := 0; i < len(value); i += 2 {
		optCodes = append(optCodes, OptionCode6(binary.BigEndian.Uint16(value[i:i+2])))
	}
	return optCodes
}

func makeDHCPReplyPacket6(pkt Packet, conf *ResponseConfig, msgType MessageType) (Packet, error) {
	tid, err := pkt.TID6()
	if err != nil {
		return nil, errors.Wrapf(err, "TID6")
	}

	originOpts := pkt.GetOption6s()
	getOption := func(opts []Option6, code OptionCode6) *Option6 {
		for _, o := range opts {
			if o.Code == code {
				return &o
			}
		}
		return nil
	}

	reqInfo := getOption(originOpts, DHCPV6_OPTION_ORO)
	if reqInfo != nil && len(reqInfo.Value) > 0 {
		reqOpts := decodeRequestOptions(reqInfo.Value)
		reqOptsStr := make([]string, len(reqOpts))
		for i, opt := range reqOpts {
			reqOptsStr[i] = opt.String()
		}
		log.Debugf("request options: %s", strings.Join(reqOptsStr, ","))
	}

	options := make([]Option6, 0)

	reqCliID := getOption(originOpts, DHCPV6_OPTION_CLIENTID)
	if reqCliID == nil {
		return nil, errors.Wrapf(errors.ErrInvalidFormat, "clientID option not found")
	}
	// copy clientID
	options = append(options, Option6{
		Code:  DHCPV6_OPTION_CLIENTID,
		Value: reqCliID.Value,
	})
	// serverID
	options = append(options, Option6{
		Code:  DHCPV6_OPTION_SERVERID,
		Value: makeServerId(conf.InterfaceMac),
	})
	// Identity Association for Non-temporary Addresses Option
	ianaOpt := getOption(originOpts, DHCPV6_OPTION_IA_NA)
	if ianaOpt == nil {
		return nil, errors.Wrapf(errors.ErrInvalidFormat, "IA_NA option not found")
	}
	// Calculate proper timing values for IA_NA
	validLifetime := uint32(conf.LeaseTime.Seconds()) // Valid lifetime should be longer than preferred
	preferredLifetime := validLifetime / 2

	options = append(options, Option6{
		Code: DHCPV6_OPTION_IA_NA,
		Value: responseIANA(ianaOpt.Value, []Option6{
			{
				Code: DHCPV6_OPTION_IAADDR,
				Value: makeIAAddr(conf.ClientIP6, preferredLifetime, validLifetime, []Option6{
					{
						Code:  DHCPV6_OPTION_STATUS_CODE,
						Value: []byte{0, 0, 'S', 'u', 'c', 'c', 'e', 's', 's'},
					},
				}),
			},
		}),
	})

	if len(conf.DNSServers6) > 0 {
		options = append(options, Option6{
			Code:  OPTION_DNS_SERVERS,
			Value: makeIPv6s(conf.DNSServers6),
		})
	}

	if len(conf.NTPServers6) > 0 {
		options = append(options, Option6{
			Code:  OPTION_SNTP_SERVERS,
			Value: makeIPv6s(conf.NTPServers6),
		})
	}

	// Handle rapid commit option for SOLICIT messages
	if pkt.Type6() == DHCPV6_SOLICIT {
		rapidCmtOpt := getOption(originOpts, DHCPV6_OPTION_RAPID_COMMIT)
		if rapidCmtOpt != nil {
			// Client requested rapid commit, respond with REPLY instead of ADVERTISE
			msgType = DHCPV6_REPLY
			options = append(options, Option6{
				Code: DHCPV6_OPTION_RAPID_COMMIT,
			})
		}
	}

	resp := NewPacket6(msgType, tid)
	resp = append(resp, optionsToBytes(options)...)

	return resp, nil
}

func EncapDHCP6RelayMsg(pkt Packet) Packet {
	relayMsg := NewRelayPacket6()
	relayOpt := Option6{
		Code:  DHCPV6_OPTION_RELAY_MSG,
		Value: pkt,
	}
	relayMsg = append(relayMsg, optionsToBytes([]Option6{relayOpt})...)
	return relayMsg
}

func DecapDHCP6RelayMsg(pkt Packet) (Packet, error) {
	options := pkt.GetOption6s()
	for _, o := range options {
		if o.Code == DHCPV6_OPTION_RELAY_MSG {
			return Packet(o.Value), nil
		}
	}
	return nil, errors.Wrapf(errors.ErrInvalidFormat, "relay message not found")
}
