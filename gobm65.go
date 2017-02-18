package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"

	flag "github.com/docker/docker/pkg/mflag"
	"github.com/tarm/serial"
)

type measurement struct {
	Header    int
	Systolic  int
	Diastolic int
	Pulse     int
	Month     int
	Day       int
	Hour      int
	Minute    int
	Year      int
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

func fetchData(dev string) (items []measurement, err error) {
	c := &serial.Config{Name: dev, Baud: 4800}

	var s *serial.Port
	s, err = serial.OpenPort(c)
	if err != nil {
		return items, err
	}

	// =================== Handshake =====================
	q := []byte("\xaa")
	//log.Printf("Query: %q\n", q)
	log.Println("Starting handshake...")
	n, err := s.Write(q)
	if err != nil {
		return items, err
	}

	buf := make([]byte, 128)
	n, err = getData(s, buf, 1)
	if err != nil {
		return items, err
	}
	if n == 1 && buf[0] == '\x55' {
		log.Println("Handshake successful.")
	} else {
		log.Printf("(%d bytes) %q\n", n, buf[:n])
		s.Close()
		return items, fmt.Errorf("handshake failed")
	}

	// =================== Desc =====================
	q = []byte("\xa4")
	//log.Printf("Query: %q\n", q)
	log.Println("Requesting device description...")
	n, err = s.Write(q)
	if err != nil {
		return items, err
	}

	n, err = getData(s, buf, 32)
	log.Printf("DESC> %q\n", buf[:n])

	// =================== Count =====================
	q = []byte("\xa2")
	//log.Printf("Query: %q\n", q)
	log.Println("Requesting data counter...")
	n, err = s.Write(q)
	if err != nil {
		return items, err
	}

	n, err = getData(s, buf, 1)
	if err != nil {
		return items, err
	}
	var nRecords int
	if n == 1 {
		log.Printf("%d item(s) available.", buf[0])
		nRecords = int(buf[0])
	} else {
		log.Printf("(%d bytes) %q\n", n, buf[:n])
		return items, fmt.Errorf("no measurement found")
	}

	// =================== Records =====================
	for i := 0; i < nRecords; i++ {
		q = []byte{'\xa3', uint8(i + 1)}
		//log.Printf("Query: %q\n", q)
		//log.Printf("Requesting measurement %d...", i+1)
		n, err = s.Write(q)
		if err != nil {
			return items, err
		}

		n, err = getData(s, buf, 9)
		//log.Printf("DESC> %q\n", buf[:n])

		var data measurement
		data.Header = int(buf[0])
		data.Systolic = int(buf[1]) + 25
		data.Diastolic = int(buf[2]) + 25
		data.Pulse = int(buf[3])
		data.Month = int(buf[4])
		data.Day = int(buf[5])
		data.Hour = int(buf[6])
		data.Minute = int(buf[7])
		data.Year = int(buf[8]) + 2000
		items = append(items, data)
	}

	s.Close()
	return items, nil
}

func loadFromJSONFile(filename string) (items []measurement, err error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return items, err
	}

	err = json.Unmarshal(data, &items)
	return items, err
}

func mergeItems(newItems, oldItems []measurement) []measurement {
	var result []measurement
	var j int
	// TODO: Would be better to compare dates and merge chronologically...
	for _, nItem := range newItems {
		result = append(result, nItem)
		if j+1 <= len(oldItems) && nItem == oldItems[j] {
			j++
		}
	}
	if j+1 <= len(oldItems) {
		result = append(result, oldItems[j:]...)
	}
	return result
}

func main() {
	inFile := flag.String([]string{"-input-file", "i"}, "", "Input JSON file")
	outFile := flag.String([]string{"-output-file", "o"}, "", "Output JSON file")
	limit := flag.Uint([]string{"-limit", "l"}, 0, "Limit number of items")
	format := flag.String([]string{"-format", "f"}, "", "Output format (csv, json)")
	avg := flag.Bool([]string{"-average", "a"}, false, "Compute average")
	merge := flag.Bool([]string{"-merge", "m"}, false,
		"Try to merge input JSON file with fetched data")
	device := flag.String([]string{"-device", "d"}, "/dev/ttyUSB0", "Serial device")

	flag.Parse()

	switch *format {
	case "":
		if *outFile == "" {
			*format = "csv"
		}
		break
	case "json", "csv":
		break
	default:
		log.Fatal("Unknown output format.  Possible choices are csv, json.")
	}

	var err error
	var items []measurement

	if *inFile == "" {
		// Read from device
		if items, err = fetchData(*device); err != nil {
			log.Fatal(err)
		}
	} else {
		// Read from file
		var fileItems []measurement
		if fileItems, err = loadFromJSONFile(*inFile); err != nil {
			log.Fatal(err)
		}
		if *merge {
			if items, err = fetchData(*device); err != nil {
				log.Fatal(err)
			}
			items = mergeItems(items, fileItems)
		} else {
			items = fileItems
		}
	}

	if *limit > 0 && len(items) > int(*limit) {
		items = items[0:*limit]
	}

	var avgMeasure measurement
	var avgCount int

	for i, data := range items {
		if *format == "csv" {
			fmt.Printf("%d;%x;%d-%02d-%02d %02d:%02d;%d;%d;%d\n",
				i+1, data.Header,
				data.Year, data.Month, data.Day,
				data.Hour, data.Minute,
				data.Systolic, data.Diastolic, data.Pulse)
		}

		avgMeasure.Systolic += data.Systolic
		avgMeasure.Diastolic += data.Diastolic
		avgMeasure.Pulse += data.Pulse
		avgCount++
	}

	if *avg && avgCount > 0 {
		avgMeasure.Systolic /= avgCount
		avgMeasure.Diastolic /= avgCount
		avgMeasure.Pulse /= avgCount

		fmt.Printf("Average: %d;%d;%d\n", avgMeasure.Systolic,
			avgMeasure.Diastolic, avgMeasure.Pulse)
	}

	if *format == "json" || *outFile != "" {
		rawJSON, err := json.MarshalIndent(items, "", "  ")
		if err != nil {
			log.Fatal("Error:", err)
		}

		if *format == "json" {
			fmt.Println(string(rawJSON))
		}
		if *outFile != "" {
			err = ioutil.WriteFile(*outFile, rawJSON, 0600)
			if err != nil {
				log.Println("Could not write output file:", err)
			}
		}
	}
}
