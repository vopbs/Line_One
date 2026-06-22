package media

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

type Codec struct {
	Name        string
	PayloadType uint8
	RTPMap      string
	MimeType    string
}

var (
	PCMU = Codec{Name: "pcmu", PayloadType: 0, RTPMap: "PCMU/8000", MimeType: "audio/PCMU"}
	PCMA = Codec{Name: "pcma", PayloadType: 8, RTPMap: "PCMA/8000", MimeType: "audio/PCMA"}
)

func CodecByName(name string) (Codec, error) {
	switch strings.ToLower(name) {
	case "", "pcmu", "g711u":
		return PCMU, nil
	case "pcma", "g711a":
		return PCMA, nil
	default:
		return Codec{}, fmt.Errorf("unsupported codec %q", name)
	}
}

func Offer(ip string, port int, codec ...Codec) string {
	selected := PCMU
	if len(codec) > 0 {
		selected = codec[0]
	}
	return fmt.Sprintf("v=0\r\n"+
		"o=- 0 0 IN IP4 %s\r\n"+
		"s=WebRTC SIP Gateway\r\n"+
		"c=IN IP4 %s\r\n"+
		"t=0 0\r\n"+
		"m=audio %d RTP/AVP %d\r\n"+
		"a=rtpmap:%d %s\r\n"+
		"a=sendrecv\r\n", ip, ip, port, selected.PayloadType, selected.PayloadType, selected.RTPMap)
}

func AudioTarget(sdp string) (*net.UDPAddr, error) {
	var ip string
	var port int
	for _, raw := range strings.Split(sdp, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "c=IN IP4 ") {
			ip = strings.TrimPrefix(line, "c=IN IP4 ")
		}
		if strings.HasPrefix(line, "m=audio ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				port, _ = strconv.Atoi(fields[1])
			}
		}
	}
	if net.ParseIP(ip) == nil || port == 0 {
		return nil, fmt.Errorf("invalid remote audio SDP")
	}
	return &net.UDPAddr{IP: net.ParseIP(ip), Port: port}, nil
}

func AcceptsCodec(sdp string, codec Codec) bool {
	payload := strconv.Itoa(int(codec.PayloadType))
	for _, raw := range strings.Split(sdp, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "m=audio ") {
			fields := strings.Fields(line)
			if len(fields) < 4 || fields[1] == "0" {
				continue
			}
			for _, offered := range fields[3:] {
				if offered == payload {
					return true
				}
			}
		}
	}
	return false
}
