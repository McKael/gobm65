package main

import (
	"io"
	"fmt"
	"log"

	"github.com/tarm/goserial"
)

type measurement struct {
	header    int
	systolic  int
	diastolic int
	pulse     int
	month     int
	day       int
	hour      int
	minute    int
	year      int
}

func getData(s io.ReadWriteCloser, buf []byte, size int) (int, error) {
	t := 0
	b := buf
	for t < size {
		n, err := s.Read(b[t:])
		if err != nil {
			log.Fatal(err) // XXX
			return t, err
		}
		//log.Printf("(%d bytes) %q\n", n, b[t:t+1])
		t = t + n
	}
	return t, nil
}

func main() {
	c := &serial.Config{Name: "/dev/ttyUSB0", Baud: 4800}
	s, err := serial.OpenPort(c)
	if err != nil {
		log.Fatal(err)
		return
	}

	q := []byte("\xaa")
	//log.Printf("Query: %q\n", q)
	log.Println("Starting handshake...")
	n, err := s.Write(q)
	if err != nil {
		log.Fatal(err)
		return
	}

	buf := make([]byte, 128)
	n, err = getData(s, buf, 1)
	if err != nil {
		log.Fatal(err)
		return
	}
	if n == 1 && buf[0] == '\x55' {
		log.Println("Handshake successful.")
	} else {
		log.Printf("(%d bytes) %q\n", n, buf[:n])
		s.Close()
		return
	}

	// =================== Desc =====================
	q = []byte("\xa4")
	//log.Printf("Query: %q\n", q)
	log.Println("Requesting device description...")
	n, err = s.Write(q)
	if err != nil {
		log.Fatal(err)
		return
	}

	n, err = getData(s, buf, 32)
	log.Printf("DESC> %q\n", buf[:n])

	// =================== Count =====================
	q = []byte("\xa2")
	//log.Printf("Query: %q\n", q)
	log.Println("Requesting data counter...")
	n, err = s.Write(q)
	if err != nil {
		log.Fatal(err)
		return
	}

	n, err = getData(s, buf, 1)
	if err != nil {
		log.Fatal(err)
		return
	}
	var nRecords int
	if n == 1 {
		log.Printf("%d item(s) available.", buf[0])
		nRecords = int(buf[0])
	} else {
		log.Printf("(%d bytes) %q\n", n, buf[:n])
		return
	}

	for i := 0; i < nRecords; i++ {
		q = []byte{'\xa3', uint8(i + 1)}
		//log.Printf("Query: %q\n", q)
		//log.Printf("Requesting measurement %d...", i+1)
		n, err = s.Write(q)
		if err != nil {
			log.Fatal(err)
			return
		}

		n, err = getData(s, buf, 9)
		//log.Printf("DESC> %q\n", buf[:n])

		var data measurement
		data.header = int(buf[0])
		data.systolic = int(buf[1]) + 25
		data.diastolic = int(buf[2]) + 25
		data.pulse = int(buf[3])
		data.month = int(buf[4])
		data.day = int(buf[5])
		data.hour = int(buf[6])
		data.minute = int(buf[7])
		data.year = int(buf[8]) + 2000
		fmt.Printf("%d;%x;%d-%02d-%02d %02d:%02d;%d;%d;%d\n",
			i+1, data.header,
			data.year, data.month, data.day,
			data.hour, data.minute,
			data.systolic, data.diastolic, data.pulse)
	}

	s.Close()
}
