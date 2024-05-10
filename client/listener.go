package client

import (
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"io"
	"os"

	"github.com/ao-data/albiondata-client/log"
	photon "github.com/ao-data/photon_spectator"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
)

type listener struct {
	handle        *pcap.Handle
	sourcePackets chan gopacket.Packet
	commands      chan photon.PhotonCommand
	displayName   string
	fragments     *photon.FragmentBuffer
	quit          chan bool
	router        *Router
}

func newListener(router *Router) *listener {
	return &listener{
		fragments: photon.NewFragmentBuffer(),
		commands:  make(chan photon.PhotonCommand, 1),
		quit:      make(chan bool, 1),
		router:    router,
	}
}

func (l *listener) startOnline(device string, port int) {
	handle, err := pcap.OpenLive(device, 2048, false, pcap.BlockForever)
	if err != nil {
		log.Panic(err)
	}
	l.handle = handle

	err = l.handle.SetBPFFilter(fmt.Sprintf("tcp port %d || udp port %d", port, port))
	if err != nil {
		log.Panic(err)
	}

	layers.RegisterUDPPortLayerType(layers.UDPPort(port), photon.PhotonLayerType)
	layers.RegisterTCPPortLayerType(layers.TCPPort(port), photon.PhotonLayerType)
	source := gopacket.NewPacketSource(l.handle, l.handle.LinkType())
	l.sourcePackets = source.Packets()

	l.displayName = fmt.Sprintf("online: %s:%d", device, port)
	l.run()
}

func (l *listener) startOfflinePcap(path string) {
	handle, err := pcap.OpenOffline(path)
	if err != nil {
		log.Panicf("Problem creating offline source. Error: %v", err)
	}
	l.handle = handle

	for _, port := range []int{5055, 5056} {
		layers.RegisterUDPPortLayerType(layers.UDPPort(port), photon.PhotonLayerType)
		layers.RegisterTCPPortLayerType(layers.TCPPort(port), photon.PhotonLayerType)
	}
	source := gopacket.NewPacketSource(handle, handle.LinkType())
	l.sourcePackets = source.Packets()

	l.displayName = fmt.Sprintf("Offline Pcap: %s", path)
	l.run()
}

func (l *listener) startOfflineCommandGob(path string) {
	// Set up packets with an empty channel
	l.sourcePackets = make(chan gopacket.Packet, 1)

	var decoder *gob.Decoder
	file, err := os.Open(path)
	if err != nil {
		log.Panic("Could not open commands input file ", err)
	} else {
		decoder = gob.NewDecoder(file)
	}

	go func() {
		for {
			command := &photon.PhotonCommand{}
			if decoder == nil {
				break
			}
			err = decoder.Decode(command)
			if err != nil {
				if err == io.EOF {
					break
				}
				log.Error("Could not decode command ", err)
				continue
			}
			l.commands <- *command
		}

		err = file.Close()
		if err != nil {
			log.Error("Could not close commands input file ", err)
		}
		log.Info("All offline commands should processed now.")
	}()

	for _, port := range []int{5055, 5056} {
		layers.RegisterUDPPortLayerType(layers.UDPPort(port), photon.PhotonLayerType)
		layers.RegisterTCPPortLayerType(layers.TCPPort(port), photon.PhotonLayerType)
	}

	l.displayName = fmt.Sprintf("Offline Commands: %s", path)
	l.run()
}

func (l *listener) run() {
	log.Debugf("Starting listener (%s)...", l.displayName)

	for {
		select {
		case <-l.quit:
			log.Debugf("Listener shutting down (%s)...", l.displayName)
			l.handle.Close()
			return
		case packet := <-l.sourcePackets:
			if packet != nil {
				l.processPacket(packet)
			} else {
				// MUST only happen with the offline processor.
				l.handle.Close()
				return
			}
		case command := <-l.commands:
			l.onReliableCommand(&command)
		}
	}
}

func (l *listener) stop() {
	l.quit <- true
	l.handle.Close()
}

func (l *listener) processPacket(packet gopacket.Packet) {
	ipLayer := packet.Layer(layers.LayerTypeIPv4)

	if ipLayer == nil {
		return
	}

	ipv4 := ipLayer.(*layers.IPv4)

	if ipLayer != nil {
		ipv4, _ = ipLayer.(*layers.IPv4)
		log.Tracef("Packet came from: %s", ipv4.SrcIP)
	}

	if ipv4.SrcIP == nil {
		log.Trace("No IPv4 detected")
		return
	}
	l.router.albionstate.GameServerIP = ipv4.SrcIP.String()
	l.router.albionstate.AODataServerID, l.router.albionstate.AODataIngestBaseURL = l.router.albionstate.GetServer()
	log.Tracef("Server ID: %s", l.router.albionstate.AODataServerID)
	log.Tracef("Using AODataIngestBaseURL: %s", l.router.albionstate.AODataIngestBaseURL)

	layer := packet.Layer(photon.PhotonLayerType)


	if layer == nil {
		return
	}

	content, _ := layer.(photon.PhotonLayer)

	for _, command := range content.Commands {
		switch command.Type {
		case photon.SendReliableType:
			l.onReliableCommand(&command)
		case photon.SendUnreliableType:
			var s = make([]byte, len(command.Data)-4)
			copy(s, command.Data[4:])
			command.Data = s
			command.Length -= 4
			command.Type = 6
			l.onReliableCommand(&command)
		case photon.SendReliableFragmentType:
			msg, _ := command.ReliableFragment()
			result := l.fragments.Offer(msg)
			if result != nil {
				l.onReliableCommand(result)
			}
		}
	}
}

func (l *listener) onReliableCommand(command *photon.PhotonCommand) {
	// Record all photon commands even if the params did not parse correctly
	if ConfigGlobal.RecordPath != "" {
		l.router.recordPhotonCommand <- *command
	}

	msg, err := command.ReliableMessage()
	if err != nil {
		if !ConfigGlobal.DebugIgnoreDecodingErrors {
			log.Debugf("Could not decode reliable message: %v - %v", err, base64.StdEncoding.EncodeToString(command.Data))
		}
		return
	}
	params := photon.DecodeReliableMessage(msg)
	if params == nil {
		if !ConfigGlobal.DebugIgnoreDecodingErrors {
			log.Debugf("ERROR: Could not decode params: [%d] (%d) (%d) %v", msg.Type, msg.ParamaterCount, len(msg.Data), base64.StdEncoding.EncodeToString(msg.Data))
		}
		return
	}

	var operation operation

	switch msg.Type {
	case photon.OperationRequest:
		operation, err = decodeRequest(params)
		if params[253] != nil {
			number := params[253].(int16)
			shouldDebug, exists := ConfigGlobal.DebugOperations[int(number)]
			if (exists && shouldDebug) || (!exists && ConfigGlobal.DebugOperationsString == "") {
				log.Debugf("OperationRequest: [%v]%v - %v", number, OperationType(number), params)
			}
		} else if !ConfigGlobal.DebugIgnoreDecodingErrors {
			log.Debugf("OperationRequest: ERROR - %v", params)
		}
	case photon.OperationResponse:
		operation, err = decodeResponse(params)
		if params[253] != nil {
			number := params[253].(int16)
			shouldDebug, exists := ConfigGlobal.DebugOperations[int(number)]
			if (exists && shouldDebug) || (!exists && ConfigGlobal.DebugOperationsString == "") {
				log.Debugf("OperationResponse: [%v]%v - %v", number, OperationType(number), params)
			}
		} else if !ConfigGlobal.DebugIgnoreDecodingErrors {
			log.Debugf("OperationResponse: ERROR - %v", params)
		}
	case photon.EventDataType:
		operation, err = decodeEvent(params)
		if params[252] != nil {
			number := params[252].(int16)
			shouldDebug, exists := ConfigGlobal.DebugEvents[int(number)]
			if (exists && shouldDebug) || (!exists && ConfigGlobal.DebugEventsString == "") {
				log.Debugf("EventDataType: [%v]%v - %v", number, EventType(number), params)
			}
		} else if !ConfigGlobal.DebugIgnoreDecodingErrors {
			log.Debugf("EventDataType: ERROR - %v", params)
		}
	default:
		err = fmt.Errorf("unsupported message type: %v, data: %v", msg.Type, base64.StdEncoding.EncodeToString(msg.Data))
	}

	if err != nil && !ConfigGlobal.DebugIgnoreDecodingErrors {
		log.Debugf("Error while decoding an event or operation: %v - params: %v", err, params)
		operation = nil
	}

	if operation != nil {
		l.router.newOperation <- operation
	}
}
