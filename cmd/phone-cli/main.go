package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/pion/rtp"

	"webrtc-sip/internal/media"
	"webrtc-sip/internal/sip"
)

func main() {
	server := flag.String("server", "127.0.0.1:5060", "SIP server host:port")
	id := flag.String("id", "", "SIP account and default caller ID")
	password := flag.String("password", os.Getenv("SIP_PASSWORD"), "SIP password, preferably use SIP_PASSWORD")
	destination := flag.String("call", "", "destination number")
	audioPath := flag.String("audio", "", "PCM WAV file to play")
	codecName := flag.String("codec", "pcmu", "pcmu or pcma")
	advertiseIP := flag.String("advertise-ip", "", "IP advertised in SIP SDP")
	sipPort := flag.Int("sip-port", 5066, "local SIP UDP port")
	rtpPort := flag.Int("rtp-port", 40000, "local RTP UDP port")
	flag.Parse()

	if *id == "" || *password == "" || *destination == "" || *audioPath == "" {
		flag.Usage()
		log.Fatal("-id, -call, -audio and password are required")
	}
	codec, err := media.CodecByName(*codecName)
	if err != nil {
		log.Fatal(err)
	}
	if *advertiseIP == "" {
		*advertiseIP, err = localIPv4(*server)
		if err != nil {
			log.Fatal(err)
		}
	}
	wav, err := media.ReadWAV(*audioPath)
	if err != nil {
		log.Fatal(err)
	}
	rtpConn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: *rtpPort})
	if err != nil {
		log.Fatal(err)
	}
	defer rtpConn.Close()

	client, err := sip.NewClient(*id, *password, *server, *advertiseIP, *sipPort)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	err = client.Register(ctx)
	cancel()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("registered %s at %s", *id, *server)

	ctx, cancel = context.WithTimeout(context.Background(), 40*time.Second)
	response, dialog, err := client.Invite(ctx, *destination, media.Offer(*advertiseIP, *rtpPort, codec))
	cancel()
	if err != nil {
		log.Fatal(err)
	}
	if response.StatusCode() < 200 || response.StatusCode() >= 300 {
		log.Fatalf("call failed: %s", response.StartLine)
	}
	target, err := media.AudioTarget(string(response.Body))
	if err != nil {
		log.Fatal(err)
	}
	target = symmetricTarget(rtpConn, target)
	log.Printf("connected, playing %s to %s using %s", *audioPath, *destination, codec.Name)
	if err := play(rtpConn, target, wav.Samples, codec); err != nil {
		log.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	_ = client.Bye(ctx, dialog)
	cancel()
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	_ = client.Unregister(ctx)
	cancel()
}

func symmetricTarget(conn *net.UDPConn, fallback *net.UDPAddr) *net.UDPAddr {
	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	defer conn.SetReadDeadline(time.Time{})
	buf := make([]byte, 2000)
	if _, source, err := conn.ReadFromUDP(buf); err == nil {
		log.Printf("using symmetric RTP target %s instead of SDP target %s", source, fallback)
		return source
	}
	return fallback
}

func play(conn *net.UDPConn, target *net.UDPAddr, samples []int16, codec media.Codec) error {
	var random [8]byte
	_, _ = rand.Read(random[:])
	sequence := binary.BigEndian.Uint16(random[:2])
	timestamp := binary.BigEndian.Uint32(random[2:6])
	ssrc := binary.BigEndian.Uint32(random[4:8])
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for offset := 0; offset < len(samples); offset += 160 {
		end := min(offset+160, len(samples))
		payload := make([]byte, 160)
		for i := range payload {
			sample := int16(0)
			if offset+i < end {
				sample = samples[offset+i]
			}
			if codec.Name == "pcma" {
				payload[i] = media.EncodePCMA(sample)
			} else {
				payload[i] = media.EncodePCMU(sample)
			}
		}
		packet := rtp.Packet{
			Header: rtp.Header{
				Version: 2, PayloadType: codec.PayloadType,
				SequenceNumber: sequence, Timestamp: timestamp, SSRC: ssrc,
			},
			Payload: payload,
		}
		raw, err := packet.Marshal()
		if err != nil {
			return err
		}
		<-ticker.C
		if _, err := conn.WriteToUDP(raw, target); err != nil {
			return err
		}
		sequence++
		timestamp += 160
	}
	return nil
}

func localIPv4(server string) (string, error) {
	addr, err := net.ResolveUDPAddr("udp4", server)
	if err != nil {
		return "", err
	}
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	ip := conn.LocalAddr().(*net.UDPAddr).IP
	if ip == nil {
		return "", fmt.Errorf("could not determine local IP")
	}
	return ip.String(), nil
}
