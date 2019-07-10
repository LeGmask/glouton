package nrpe

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"log"
	"net"
)

type reducedPacket struct {
	packetType int16
	resultCode int16
	buffer     string
}

func handleConnection(c net.Conn) {
	a, err := decode(c)
	if err != nil {
		log.Println(err)
		c.Close()
		return
	}
	log.Printf("packet_type : %v, buffer : %v\n", a.packetType, a.buffer)

	var answer reducedPacket
	answer.buffer = "connection successful"

	b, err := encodeV3(answer)
	if err != nil {
		log.Println(err)
		c.Close()
		return
	}
	c.Write(b)
	c.Close()
}

func decode(r io.Reader) (reducedPacket, error) {
	b := make([]byte, 16)
	r.Read(b)
	var packetVersion int16
	var bufferlength int32
	var a reducedPacket

	buf := bytes.NewReader(b[:2])
	err := binary.Read(buf, binary.BigEndian, &packetVersion)
	if err != nil {
		err = errors.New("binary.Read failed for packet_version")
		return a, err
	}

	if packetVersion == 3 {
		buf = bytes.NewReader(b[12:16])
		err = binary.Read(buf, binary.BigEndian, &bufferlength)
		if err != nil {
			err = errors.New("binary.Read failed for buffer_length")
			return a, err
		}
	}
	if packetVersion == 2 {
		bufferlength = 1017
	}

	//test value CRC32
	var crc32value uint32
	buf = bytes.NewReader(b[4:8])
	err = binary.Read(buf, binary.BigEndian, &crc32value)
	if err != nil {
		err = errors.New("binary.Read failed for packet_type")
		return a, err
	}
	d := make([]byte, bufferlength+3)
	r.Read(d)
	v := make([]byte, 19+bufferlength)
	copy(v[:16], b)
	copy(v[16:], d)
	v[4] = 0
	v[5] = 0
	v[6] = 0
	v[7] = 0
	if crc32.ChecksumIEEE(v) != crc32value {
		return a, errors.New("wrong value for crc32")
	}
	//affectation du buffer
	i := bytes.IndexByte(d, 0x0)
	d = d[:i]
	if packetVersion == 3 {
		a.buffer = string(d)
	}
	if packetVersion == 2 {
		a.buffer = string(b[10:]) + string(d)
	}

	buf = bytes.NewReader(b[2:4])
	err = binary.Read(buf, binary.BigEndian, &a.packetType)
	if err != nil {
		err = errors.New("binary.Read failed for packet_type")
		return a, err
	}

	buf = bytes.NewReader(b[8:10])
	err = binary.Read(buf, binary.BigEndian, &a.resultCode)
	if err != nil {
		err = errors.New("binary.Read failed for result_code")
		return a, err
	}

	return a, nil
}

func encodeV2(answer reducedPacket, randBytes [2]byte) ([]byte, error) {
	answer.packetType = 2

	b := make([]byte, 1036)
	b[1] = 0x02

	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.BigEndian, &answer.packetType)
	if err != nil {
		log.Println("binary.Write failed for packet_type:", err)
		return b, err
	}
	copy(b[2:4], buf.Bytes())

	buf = new(bytes.Buffer)
	err = binary.Write(buf, binary.BigEndian, &answer.resultCode)
	if err != nil {
		log.Println("binary.Write failed for result_code:", err)
		return b, err
	}
	copy(b[8:10], buf.Bytes())

	copy(b[10:10+len(answer.buffer)], []byte(answer.buffer))
	b[1034] = randBytes[0]
	b[1035] = randBytes[1]

	crc32Value := crc32.ChecksumIEEE(b)
	buf = new(bytes.Buffer)
	err = binary.Write(buf, binary.BigEndian, &crc32Value)
	if err != nil {
		log.Println("binary.Write failed for crc32_value:", err)
		return b, err
	}
	copy(b[4:8], buf.Bytes())

	return b, nil
}

func encodeV3(answer reducedPacket) ([]byte, error) {
	packetVersion := int16(3)
	answer.packetType = 2
	bufferLength := int32(len(answer.buffer))
	b2 := make([]byte, 19+len(answer.buffer))

	buf2 := new(bytes.Buffer)
	err := binary.Write(buf2, binary.BigEndian, &packetVersion)
	if err != nil {
		log.Println("binary.Write failed for packet_version:", err)
		return b2, err
	}
	copy(b2[:2], buf2.Bytes())

	buf2 = new(bytes.Buffer)
	err = binary.Write(buf2, binary.BigEndian, &answer.packetType)
	if err != nil {
		log.Println("binary.Write failed for packet_type:", err)
		return b2, err
	}
	copy(b2[2:4], buf2.Bytes())

	buf2 = new(bytes.Buffer)
	err = binary.Write(buf2, binary.BigEndian, &bufferLength)
	if err != nil {
		log.Println("binary.Write failed for buffer_length:", err)
		return b2, err
	}
	copy(b2[12:16], buf2.Bytes())

	buf2 = new(bytes.Buffer)
	err = binary.Write(buf2, binary.BigEndian, &answer.resultCode)
	if err != nil {
		log.Println("binary.Write failed for result_code:", err)
		return b2, err
	}
	copy(b2[8:10], buf2.Bytes())

	buf2 = new(bytes.Buffer)
	copy(b2[16:16+len(answer.buffer)], []byte(answer.buffer))

	crc32Value := crc32.ChecksumIEEE(b2)
	buf2 = new(bytes.Buffer)
	err = binary.Write(buf2, binary.BigEndian, &crc32Value)
	if err != nil {
		log.Println("binary.Write failed for crc32_value:", err)
		return b2, err
	}
	copy(b2[4:8], buf2.Bytes())
	return b2, nil
}

//Run start a connection with a nrpe server
func Run(port string) {
	l, err := net.Listen("tcp4", port)
	if err != nil {
		log.Println(err)
		return
	}
	defer l.Close()

	for {
		c, err := l.Accept()
		if err != nil {
			log.Println(err)
			return
		}
		go handleConnection(c)
	}
}
