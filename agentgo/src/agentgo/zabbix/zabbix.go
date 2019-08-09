package zabbix

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

type packetStruct struct {
	version int8
	key     string
	args    []string
}

type callback func(key string, args []string) string

func handleConnection(c io.ReadWriteCloser, cb callback) {
	decodedRequest, err := decode(c)
	if err != nil {
		log.Printf("Unable to decode Zabbix packet: %v", err)
		c.Close()
		return
	}

	var answer packetStruct
	answer.key = cb(decodedRequest.key, decodedRequest.args)
	answer.version = decodedRequest.version

	var encodedAnswer []byte
	if answer.version == 1 {
		encodedAnswer, err = encodev1(answer)
	}
	if err != nil {
		log.Println(err)
		c.Close()
		return
	}

	_, err = c.Write(encodedAnswer)
	if err != nil {
		log.Printf("Answer writing failed: %v", err)
	}

	c.Close()
}

func decode(r io.Reader) (packetStruct, error) {
	packetHead := make([]byte, 13)
	_, err := r.Read(packetHead)
	if err != nil {
		return packetStruct{}, err
	}
	var decodedPacket packetStruct
	header := packetHead[0:4]
	buf := bytes.NewReader(packetHead[4:])

	if bytes.Compare(header, []byte("ZBXD")) != 0 {
		err = fmt.Errorf("wrong packet header")
		return decodedPacket, err
	}
	err = binary.Read(buf, binary.LittleEndian, &decodedPacket.version)
	if err != nil {
		err = fmt.Errorf("binary.Read failed for packet_version: %v", err)
		return decodedPacket, err
	}
	var dataLength int64
	err = binary.Read(buf, binary.LittleEndian, &dataLength)
	if err != nil {
		err = fmt.Errorf("binary.Read failed for packet_version: %v", err)
		return decodedPacket, err
	}
	packetData := make([]byte, dataLength)
	_, err = r.Read(packetData)
	if err != nil {
		err = fmt.Errorf("r.Read failed for data: %v", err)
		return decodedPacket, err
	}
	strPacketData := string(packetData)
	decodedPacket.key, decodedPacket.args, err = splitData(strPacketData)

	return decodedPacket, err
}

func splitData(request string) (string, []string, error) {
	var args []string
	i := strings.Index(request, "[")
	if i == -1 {
		return request, args, nil
	}
	newrequest := strings.Replace(request, " ", "", -1)
	key := newrequest[0:i]
	if string(newrequest[len(newrequest)-1]) != "]" {
		return key, args, errors.New("missing closing bracket at the end")
	}
	joinArgs := newrequest[i+1 : len(newrequest)-1]
	if len(joinArgs) == 0 {
		return key, []string{""}, nil
	}
	var j int
	var inBrackets bool
	for k, s := range joinArgs {
		if inBrackets {
			if string(s) == "]" && k == len(joinArgs)-1 {
				inBrackets = false
				args = append(args, string(joinArgs[j:k]))
				j = k + 1
				continue
			}
			if string(s) == "," && string(joinArgs[k-1]) == "]" {
				inBrackets = false
				args = append(args, string(joinArgs[j:k-1]))
				j = k + 1
			}
		} else {
			if string(s) == "[" && j == k {
				inBrackets = true
				j = k + 1
			}
			if string(s) == "," {
				if string(joinArgs[j]) == `"` && string(joinArgs[k-1]) == `"` {
					args = append(args, string(joinArgs[j+1:k-1]))
				} else {
					args = append(args, string(joinArgs[j:k]))
				}
				j = k + 1
			}
		}
	}
	if inBrackets {
		err := errors.New("unmatched opening brackets")
		return key, args, err
	}
	if j == len(joinArgs) {
		if string(joinArgs[len(joinArgs)-1]) == "," {
			args = append(args, "")
		}
	} else {
		if string(joinArgs[j]) == `"` && string(joinArgs[len(joinArgs)-1]) == `"` {
			args = append(args, string(joinArgs[j+1:len(joinArgs)-1]))
		} else {
			args = append(args, string(joinArgs[j:]))
		}
	}
	return key, args, nil
}

func encodev1(decodedPacket packetStruct) ([]byte, error) {
	var dataLength = int64(len(decodedPacket.key))
	encodedPacket := make([]byte, 13+dataLength)

	copy(encodedPacket[0:4], []byte("ZBXD"))

	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.LittleEndian, &decodedPacket.version)
	if err != nil {
		err = fmt.Errorf("binary.Write failed for result_code: %v", err)
		return encodedPacket, err
	}
	copy(encodedPacket[4:5], buf.Bytes())

	buf = new(bytes.Buffer)
	err = binary.Write(buf, binary.LittleEndian, &dataLength)
	if err != nil {
		err = fmt.Errorf("binary.Write failed for data_length: %v", err)
		return encodedPacket, err
	}
	copy(encodedPacket[5:13], buf.Bytes())

	copy(encodedPacket[13:], []byte(decodedPacket.key))
	return encodedPacket, nil
}

//Run starts a connection with a zabbix server
func Run(ctx context.Context, port string, cb callback, useTLS bool) {
	tcpAdress, err := net.ResolveTCPAddr("tcp4", port)
	if err != nil {
		log.Println(err)
		return
	}
	l, err := net.ListenTCP("tcp4", tcpAdress)

	if err != nil {
		log.Println(err)
		return
	}
	defer l.Close()
	lWrap := net.Listener(l)
	if useTLS {
	}

	var wg sync.WaitGroup
	for {
		err := l.SetDeadline(time.Now().Add(time.Second))
		if err != nil {
			log.Printf("Nrpe: setDeadline on listener failed: %v", err)
			break
		}
		c, err := lWrap.Accept()
		if ctx.Err() != nil {
			break
		}
		if errNet, ok := err.(net.Error); ok && errNet.Timeout() {
			continue
		}
		if err != nil {
			log.Printf("Nrpe accept failed: %v", err)
			break
		}

		err = c.SetDeadline(time.Now().Add(time.Second * 10))
		if err != nil {
			log.Printf("Nrpe: setDeadline on connection failed: %v", err)
			break
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			handleConnection(c, cb)
		}()
	}
	wg.Wait()
}
