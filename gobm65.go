// Copyright (C) 2015-2017 Mikael Berthe <mikael@lilotux.net>. All rights reserved.
// Use of this source code is governed by the MIT license,
// which can be found in the LICENSE file.
//
// Thanks to atbrask's blog post <http://www.atbrask.dk/?p=98> for the
// protocol details.

// gobm65 is a Beurer BM65 Blood Pressure Monitor CLI reader.

package main

// Installation:
//
// % go get hg.lilotux.net/golang/mikael/gobm65
//
// Examples:
//
// Get help:
// % gobm65 --help
//
// Get records and display the average:
// % gobm65 --average
//
// Display the latest 3 records with the average:
// % gobm65 -l 3 --average
// Display all records since a specific date:
// % gobm65 --since "2016-06-01"
// Display all records of the last 7 days:
// % gobm65 --since "$(date "+%F" -d "7 days ago")"
//
// Display the last/first 10 records in JSON:
// % gobm65 -l 10 --format json
//
// Save the records to a JSON file:
// % gobm65 -o data_u2.json
//
// Read a JSON file and display average of the last 3 records:
// % gobm65 -i data_u2.json -l 3 --average
// Read a JSON file, merge with device records, and save to another file:
// % gobm65 -i data_u2.json --merge -o data_u2-new.json

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"time"

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

func parseDate(dateStr string) (date time.Time, err error) {
	if dateStr == "" {
		return
	}

	var yy, mm, dd, h, m, s int
	n, e := fmt.Sscanf(dateStr, "%d-%d-%d %d:%d:%d", &yy, &mm, &dd, &h, &m, &s)
	if e != nil && n < 3 {
		err = e
		return
	}
	if n < 6 {
		log.Printf("Date parsed with only %d fields\n", n)
	}
	date = time.Date(yy, time.Month(mm), dd, h, m, s, 0, time.Local)
	return
}

func main() {
	inFile := flag.String([]string{"-input-file", "i"}, "", "Input JSON file")
	outFile := flag.String([]string{"-output-file", "o"}, "", "Output JSON file")
	limit := flag.Uint([]string{"-limit", "l"}, 0, "Limit number of items to N first")
	since := flag.String([]string{"-since"}, "",
		"Filter records from date (YYYY-mm-dd HH:MM:SS)")
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

	startDate, err := parseDate(*since)
	if err != nil {
		log.Fatal("Could not parse date: ", err)
	}

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

	if !startDate.IsZero() {
		log.Printf("Filtering out records before %v...\n", startDate)
		for i := range items {
			iDate := time.Date(items[i].Year, time.Month(items[i].Month),
				items[i].Day, items[i].Hour, items[i].Minute, 0, 0,
				time.Local)
			if iDate.Sub(startDate) < 0 {
				items = items[0:i]
				break
			}
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
