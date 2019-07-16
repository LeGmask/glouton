package nrpe

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

type reducedPacket struct {
	packetVersion int16
	packetType    int16
	resultCode    int16
	buffer        string
}

type callback func(command string) (string, int16)

func handleConnection(c io.ReadWriteCloser, cb callback) {
	decodedRequest, err := decode(c)
	if err != nil {
		log.Println(err)
		c.Close()
		return
	}
	log.Printf("packet_version : %v, packet_type : %v, buffer : %v\n", decodedRequest.packetVersion, decodedRequest.packetType, decodedRequest.buffer)

	var answer reducedPacket
	answer.buffer, answer.resultCode = cb(decodedRequest.buffer)
	answer.packetVersion = decodedRequest.packetVersion

	var encodedAnswer []byte
	if answer.packetVersion == 3 {
		encodedAnswer, err = encodeV3(answer)
	} else {
		encodedAnswer, err = encodeV2(answer, [2]byte{0x53, 0x51})
	}
	if err != nil {
		log.Println(err)
		c.Close()
		return
	}
	_, err = c.Write(encodedAnswer)
	if err != nil {
		log.Println(err)
	}
	c.Close()
}

func decode(r io.Reader) (reducedPacket, error) {
	packetHead := make([]byte, 16)
	_, err := r.Read(packetHead)
	if err != nil {
		log.Println(err)
	}
	var bufferlength int32
	var decodedPacket reducedPacket

	buf := bytes.NewReader(packetHead)
	err = binary.Read(buf, binary.BigEndian, &decodedPacket.packetVersion)
	if err != nil {
		err = errors.New("binary.Read failed for packet_version")
		return decodedPacket, err
	}
	err = binary.Read(buf, binary.BigEndian, &decodedPacket.packetType)
	if err != nil {
		err = errors.New("binary.Read failed for packet_type")
		return decodedPacket, err
	}
	var crc32value uint32
	err = binary.Read(buf, binary.BigEndian, &crc32value)
	if err != nil {
		err = errors.New("binary.Read failed for packet_type")
		return decodedPacket, err
	}
	err = binary.Read(buf, binary.BigEndian, &decodedPacket.resultCode)
	if err != nil {
		err = errors.New("binary.Read failed for result_code")
		return decodedPacket, err
	}

	if decodedPacket.packetVersion == 3 {
		var uselessvariable int16
		err = binary.Read(buf, binary.BigEndian, &uselessvariable)
		if err != nil {
			err = errors.New("binary.Read failed for alignment")
			return decodedPacket, err
		}
		err = binary.Read(buf, binary.BigEndian, &bufferlength)
		if err != nil {
			err = errors.New("binary.Read failed for buffer_length")
			return decodedPacket, err
		}
	}
	if decodedPacket.packetVersion == 2 {
		bufferlength = 1017
	}

	packetBuffer := make([]byte, bufferlength+3)
	_, err = r.Read(packetBuffer)
	if err != nil {
		log.Println(err)
	}
	//test value CRC32
	completePacket := make([]byte, 19+bufferlength)
	copy(completePacket[:16], packetHead)
	copy(completePacket[16:], packetBuffer)
	completePacket[4] = 0
	completePacket[5] = 0
	completePacket[6] = 0
	completePacket[7] = 0
	if crc32.ChecksumIEEE(completePacket) != crc32value {
		return decodedPacket, errors.New("wrong value for crc32")
	}

	i := bytes.IndexByte(packetBuffer, 0x0)
	if decodedPacket.packetVersion == 3 {
		packetBuffer = packetBuffer[:i]
		decodedPacket.buffer = string(packetBuffer)
	}
	if decodedPacket.packetVersion == 2 {
		packetBuffer = packetBuffer[:i]
		decodedPacket.buffer = string(packetHead[10:]) + string(packetBuffer)
	}

	return decodedPacket, nil
}

func encodeV2(decodedPacket reducedPacket, randBytes [2]byte) ([]byte, error) {
	decodedPacket.packetType = 2

	encodedPacket := make([]byte, 1036)
	encodedPacket[1] = 0x02 //version 2 encoding

	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.BigEndian, &decodedPacket.packetType)
	if err != nil {
		log.Println("binary.Write failed for packet_type:", err)
		return encodedPacket, err
	}
	copy(encodedPacket[2:4], buf.Bytes())

	buf = new(bytes.Buffer)
	err = binary.Write(buf, binary.BigEndian, &decodedPacket.resultCode)
	if err != nil {
		log.Println("binary.Write failed for result_code:", err)
		return encodedPacket, err
	}
	copy(encodedPacket[8:10], buf.Bytes())

	copy(encodedPacket[10:10+len(decodedPacket.buffer)], []byte(decodedPacket.buffer))
	encodedPacket[1034] = randBytes[0] //random bytes encoding
	encodedPacket[1035] = randBytes[1]

	crc32Value := crc32.ChecksumIEEE(encodedPacket)
	buf = new(bytes.Buffer)
	err = binary.Write(buf, binary.BigEndian, &crc32Value)
	if err != nil {
		log.Println("binary.Write failed for crc32_value:", err)
		return encodedPacket, err
	}
	copy(encodedPacket[4:8], buf.Bytes())

	return encodedPacket, nil
}

func encodeV3(decodedPacket reducedPacket) ([]byte, error) {
	decodedPacket.packetType = 2
	bufferLength := int32(len(decodedPacket.buffer))
	encodedPacket := make([]byte, 19+len(decodedPacket.buffer))

	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.BigEndian, &decodedPacket.packetVersion)
	if err != nil {
		log.Println("binary.Write failed for packet_version:", err)
		return encodedPacket, err
	}
	copy(encodedPacket[:2], buf.Bytes())

	buf = new(bytes.Buffer)
	err = binary.Write(buf, binary.BigEndian, &decodedPacket.packetType)
	if err != nil {
		log.Println("binary.Write failed for packet_type:", err)
		return encodedPacket, err
	}
	copy(encodedPacket[2:4], buf.Bytes())

	buf = new(bytes.Buffer)
	err = binary.Write(buf, binary.BigEndian, &bufferLength)
	if err != nil {
		log.Println("binary.Write failed for buffer_length:", err)
		return encodedPacket, err
	}
	copy(encodedPacket[12:16], buf.Bytes())

	buf = new(bytes.Buffer)
	err = binary.Write(buf, binary.BigEndian, &decodedPacket.resultCode)
	if err != nil {
		log.Println("binary.Write failed for result_code:", err)
		return encodedPacket, err
	}
	copy(encodedPacket[8:10], buf.Bytes())

	buf = new(bytes.Buffer)
	copy(encodedPacket[16:16+len(decodedPacket.buffer)], []byte(decodedPacket.buffer))

	crc32Value := crc32.ChecksumIEEE(encodedPacket)
	buf = new(bytes.Buffer)
	err = binary.Write(buf, binary.BigEndian, &crc32Value)
	if err != nil {
		log.Println("binary.Write failed for crc32_value:", err)
		return encodedPacket, err
	}
	copy(encodedPacket[4:8], buf.Bytes())
	return encodedPacket, nil
}

//Run start a connection with a nrpe server
func Run(ctx context.Context, port string, cb callback) {
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

	var wg sync.WaitGroup
	for {
		err := l.SetDeadline(time.Now().Add(time.Second))
		if err != nil {
			log.Printf("Nrpe: setDeadline failed: %v", err)
		}
		c, err := l.AcceptTCP()
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

		wg.Add(1)
		go func() {
			defer wg.Done()
			handleConnection(c, cb)
		}()
	}
	wg.Wait()
}
