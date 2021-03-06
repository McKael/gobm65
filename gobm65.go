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
// ... display more statistics:
// % gobm65 --stats
// ... also display World Health Organization classification:
// % gobm65 --stats --class
//
// Display the latest 3 records with the average:
// % gobm65 -l 3 --average
// Display all records since a specific date:
// % gobm65 --since "2016-06-01"
// Display all records before a specific date:
// % gobm65 --to-date "2016-06-30"
// Display all records of the last 7 days:
// % gobm65 --since "$(date "+%F" -d "7 days ago")"
//
// Display statistics for morning records:
// % gobm65 --from-time 06:00 --to-time 12:00 --stats
// One can invert times to get night data:
// % gobm65 --from-time 21:00 --to-time 09:00
//
// Display the last/first 10 records in JSON:
// % gobm65 -l 10 --format json
//
// Save the records to a JSON file:
// % gobm65 -o data_u2.json
//
// Read a JSON file and display average of the last 3 records:
// % gobm65 -i data_u2.json -l 3 --average
// % gobm65 -i data_u2.json -l 3 --stats
// Read a JSON file, merge with device records, and save to another file:
// % gobm65 -i data_u2.json --merge -o data_u2-new.json
//
// Data from several JSON files can be merged, files are separated with a ';':
// % gobm65 -i "data_u0.json;data_u1.json;data_u2.json"

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	flag "github.com/spf13/pflag"
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

type simpleTime struct {
	hour, minute int
}

// World Heath Organization blood pressure classification
const (
	BPOptimal              = iota // < 120,80:  Optimal
	BPNormal                      // < 130,85:  Normal
	BPHighNormal                  // < 140,90:  High-Normal
	BPMildHypertension            // < 160,100: Grade 1 Mild Hypertension
	BPModerateHypertension        // < 180,110: Grade 2 Moderate Hypertension
	BPSevereHypertension          // >=180,110: Grade 3 Severe Hypertension
)

// Special cases that do not fit in the previous classification
const (
	// >=140, <90: Isolated Systolic Hypertension
	IsolatedSystolicHypertension = 1
)

// WHOPressureClassification contains the World Health Organization blood
// pressure categories
var WHOPressureClassification = []string{
	"Optimal",
	"Normal",
	"High-Normal",
	"Mild Hypertension",
	"Moderate Hypertension",
	"Severe Hypertension",
}

// WHOPressureFlag is an array of special cases
var WHOPressureFlag = []string{
	"",
	"Isolated Systolic Hypertension",
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
		t += n
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
	return mergeItems(items, []measurement{}), nil
}

func loadFromJSONFile(filename string) (items []measurement, err error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return items, err
	}

	err = json.Unmarshal(data, &items)
	return items, err
}

func loadFromJSONFiles(files string) (items []measurement, err error) {
	filenames := strings.Split(files, ";")

	for _, f := range filenames {
		if f == "" {
			continue
		}
		records, err := loadFromJSONFile(f)
		if err != nil {
			return items, err
		}
		items = mergeItems(records, items)
	}

	return
}

func mergeItems(newItems, oldItems []measurement) []measurement {
	var result []measurement

	// Sort method: isLater returns true if mi's date is later or
	// equal to mj's date.
	isLater := func(mi, mj measurement) bool {
		switch {
		case mi.Year < mj.Year:
			return false
		case mi.Year > mj.Year:
			return true
		case mi.Month < mj.Month:
			return false
		case mi.Month > mj.Month:
			return true
		case mi.Day < mj.Day:
			return false
		case mi.Day > mj.Day:
			return true
		case mi.Hour < mj.Hour:
			return false
		case mi.Hour > mj.Hour:
			return true
		case mi.Minute < mj.Minute:
			return false
		default:
			return true
		}
	}

	// Note that sort.Slice was introduced in go 1.8
	sort.Slice(oldItems, func(i, j int) bool {
		return isLater(oldItems[i], oldItems[j])
	})
	sort.Slice(newItems, func(i, j int) bool {
		return isLater(newItems[i], newItems[j])
	})

	// insertIfMissing inserts a measurement into a sorted slice
	insertIfMissing := func(l []measurement, m measurement) []measurement {
		var later bool
		var i int
		for i = range l {
			later = isLater(l[i], m)
			if !later {
				break
			}
			if l[i] == m { // Duplicate
				return l
			}
		}
		if later {
			return append(l, m)
		}

		return append(l[:i], append([]measurement{m}, l[i:]...)...)
	}

	for _, item := range newItems {
		result = insertIfMissing(result, item)
	}
	for _, item := range oldItems {
		result = insertIfMissing(result, item)
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

func parseTime(timeStr string) (t simpleTime, err error) {
	_, err = fmt.Sscanf(timeStr, "%d:%d", &t.hour, &t.minute)
	return
}

func average(items []measurement) (measurement, error) {
	var avgMeasure measurement
	var avgCount int

	for _, data := range items {
		avgMeasure.Systolic += data.Systolic
		avgMeasure.Diastolic += data.Diastolic
		avgMeasure.Pulse += data.Pulse
		avgCount++
	}

	roundDivision := func(a, b int) int {
		return int(0.5 + float64(a)/float64(b))
	}

	if avgCount == 0 {
		return avgMeasure, fmt.Errorf("cannot compute average: empty set")
	}

	avgMeasure.Systolic = roundDivision(avgMeasure.Systolic, avgCount)
	avgMeasure.Diastolic = roundDivision(avgMeasure.Diastolic, avgCount)
	avgMeasure.Pulse = roundDivision(avgMeasure.Pulse, avgCount)

	return avgMeasure, nil
}

func intMedian(numbers []int) int {
	middle := len(numbers) / 2
	med := numbers[middle]
	if len(numbers)%2 == 0 {
		med = (med + numbers[middle-1]) / 2
	}
	return med
}

func median(items []measurement) (measurement, error) {
	var med measurement
	if len(items) == 0 {
		return med, fmt.Errorf("cannot compute average: empty set")
	}

	var sys, dia, pul []int
	for _, data := range items {
		sys = append(sys, data.Systolic)
		dia = append(dia, data.Diastolic)
		pul = append(pul, data.Pulse)
	}

	sort.Ints(sys)
	sort.Ints(dia)
	sort.Ints(pul)

	med.Systolic = intMedian(sys)
	med.Diastolic = intMedian(dia)
	med.Pulse = intMedian(pul)

	return med, nil
}

func stdDeviation(items []measurement) (measurement, error) {
	var sDev measurement

	if len(items) <= 1 {
		return sDev, fmt.Errorf("cannot compute deviation: set too small")
	}

	var sumSys, sumDia, sumPul float64
	avg, err := average(items)
	if err != nil {
		return sDev, err
	}

	for _, data := range items {
		sumSys += math.Pow(float64(data.Systolic-avg.Systolic), 2)
		sumDia += math.Pow(float64(data.Diastolic-avg.Diastolic), 2)
		sumPul += math.Pow(float64(data.Pulse-avg.Pulse), 2)
	}

	sDev.Systolic = int(math.Sqrt(sumSys / float64(len(items))))
	sDev.Diastolic = int(math.Sqrt(sumDia / float64(len(items))))
	sDev.Pulse = int(math.Sqrt(sumPul / float64(len(items))))

	return sDev, nil
}

// avgAbsoluteDeviation computes the average absolute deviation (or Mean
// Absolute Deviation) of the measurement set
func avgAbsoluteDeviation(items []measurement) (measurement, error) {
	var dev measurement

	if len(items) <= 1 {
		return dev, fmt.Errorf("cannot compute deviation: set too small")
	}

	var sumSys, sumDia, sumPul float64
	avg, err := average(items)
	if err != nil {
		return dev, err
	}

	for _, data := range items {
		sumSys += math.Abs(float64(data.Systolic - avg.Systolic))
		sumDia += math.Abs(float64(data.Diastolic - avg.Diastolic))
		sumPul += math.Abs(float64(data.Pulse - avg.Pulse))
	}

	dev.Systolic = int(sumSys / float64(len(items)))
	dev.Diastolic = int(sumDia / float64(len(items)))
	dev.Pulse = int(sumPul / float64(len(items)))

	return dev, nil
}

func (m measurement) WHOClass() (int, int) {
	flag := 0

	if m.Systolic >= 140 && m.Diastolic < 90 {
		flag = IsolatedSystolicHypertension
	}

	switch {
	case m.Systolic < 120 && m.Diastolic < 80:
		return BPOptimal, flag
	case m.Systolic < 130 && m.Diastolic < 85:
		return BPNormal, flag
	case m.Systolic < 140 && m.Diastolic < 90:
		return BPHighNormal, flag
	case m.Systolic < 160 && m.Diastolic < 100:
		return BPMildHypertension, flag
	case m.Systolic < 180 && m.Diastolic < 110:
		return BPModerateHypertension, flag
	}
	return BPSevereHypertension, flag
}

func (m measurement) WHOClassString() string {
	flagStr := ""
	class, flag := m.WHOClass()
	if flag == IsolatedSystolicHypertension {
		flagStr = " (" + WHOPressureFlag[flag] + ")"
	}
	return WHOPressureClassification[class] + flagStr
}

func displayWHOClassStats(items []measurement) {
	sum := 0.0
	classes := make(map[int]int)
	for _, m := range items {
		s, flag := m.WHOClass()
		classes[s]++
		sum += float64(s)
		if flag == IsolatedSystolicHypertension {
			sum += 0.5
		}
	}

	avg := sum / float64(len(items))
	fmt.Fprintf(os.Stderr, "Average WHO classification: %s (%.2f)\n",
		WHOPressureClassification[int(0.5+avg)], avg)

	for c := range WHOPressureClassification {
		fmt.Fprintf(os.Stderr, " . %21s: %3d (%d%%)\n",
			WHOPressureClassification[c], classes[c],
			classes[c]*100/len(items))
	}
}

func main() {
	inFile := flag.StringP("input-file", "i", "", "Input JSON file")
	outFile := flag.StringP("output-file", "o", "", "Output JSON file")
	limit := flag.UintP("limit", "l", 0, "Limit number of items to N first")
	toDate := flag.String("to-date", "", "Filter records before date (YYYY-mm-dd HH:MM:SS)")
	fromDate := flag.String("from-date", "", "Filter records from date (YYYY-mm-dd HH:MM:SS)")
	format := flag.StringP("format", "f", "", "Output format (csv, json)")
	avg := flag.BoolP("average", "a", false, "Compute average")
	stats := flag.Bool("stats", false, "Compute statistics")
	whoClass := flag.BoolP("class", "c", false, "Display WHO classification")
	merge := flag.BoolP("merge", "m", false, "Try to merge input JSON file with fetched data")
	device := flag.StringP("device", "d", "/dev/ttyUSB0", "Serial device")
	fromTime := flag.String("from-time", "", "Select records after time (HH:MM)")
	toTime := flag.String("to-time", "", "Select records bofore time (HH:MM)")

	flag.StringVar(fromDate, "since", "", "Same as --from-date")

	var startTime, endTime simpleTime

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

	if *fromTime != "" {
		if t, err := parseTime(*fromTime); err != nil {
			log.Fatal("Cannot parse 'from' time: ", err)
		} else {
			startTime = t
		}
	}
	if *toTime != "" {
		if t, err := parseTime(*toTime); err != nil {
			log.Fatal("Cannot parse 'to' time: ", err)
		} else {
			endTime = t
		}
	}

	startDate, err := parseDate(*fromDate)
	if err != nil {
		log.Fatal("Could not parse date: ", err)
	}

	endDate, err := parseDate(*toDate)
	if err != nil {
		log.Fatal("Could not parse date: ", err)
	}

	var items []measurement

	// Read data

	if *inFile == "" {
		// Read from device
		if items, err = fetchData(*device); err != nil {
			log.Fatal(err)
		}
	} else {
		// Read from file
		var fileItems []measurement
		if fileItems, err = loadFromJSONFiles(*inFile); err != nil {
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

	// Apply filters

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

	if !endDate.IsZero() {
		log.Printf("Filtering out records after %v...\n", endDate)
		for i := range items {
			iDate := time.Date(items[i].Year, time.Month(items[i].Month),
				items[i].Day, items[i].Hour, items[i].Minute, 0, 0,
				time.Local)
			if iDate.Sub(endDate) <= 0 {
				items = items[i:]
				break
			}
		}
	}

	if *fromTime != "" || *toTime != "" {
		log.Println("Filtering hours...")

		compare := func(m measurement, t simpleTime) int {
			if m.Hour*60+m.Minute < t.hour*60+t.minute {
				return -1
			}
			if m.Hour*60+m.Minute > t.hour*60+t.minute {
				return 1
			}
			return 0
		}

		inv := false
		if *fromTime != "" && *toTime != "" &&
			startTime.hour*60+startTime.minute > endTime.hour*60+endTime.minute {
			inv = true
		}

		var newItems []measurement
		for _, data := range items {
			if inv {
				if compare(data, startTime) == -1 && compare(data, endTime) == 1 {
					continue
				}
				newItems = append(newItems, data)
				continue
			}
			if *fromTime != "" && compare(data, startTime) == -1 {
				continue
			}
			if *toTime != "" && compare(data, endTime) == 1 {
				continue
			}
			newItems = append(newItems, data)
		}
		items = newItems
	}

	if *limit > 0 && len(items) > int(*limit) {
		items = items[0:*limit]
	}

	// Done with filtering

	if *format == "csv" {
		for i, data := range items {
			fmt.Printf("%d;%x;%d-%02d-%02d %02d:%02d;%d;%d;%d",
				i+1, data.Header,
				data.Year, data.Month, data.Day,
				data.Hour, data.Minute,
				data.Systolic, data.Diastolic, data.Pulse)
			if *whoClass {
				fmt.Printf(";%s", data.WHOClassString())
			}
			fmt.Println()
		}
	}

	if *stats {
		*avg = true
	}

	if *avg && len(items) > 0 {
		avgMeasure, err := average(items)
		if err != nil {
			log.Println("Error:", err)
		} else {
			fmt.Fprintf(os.Stderr, "Average: %d;%d;%d",
				avgMeasure.Systolic, avgMeasure.Diastolic,
				avgMeasure.Pulse)
			if *whoClass {
				fmt.Fprintf(os.Stderr, "  [%s]",
					avgMeasure.WHOClassString())
			}
			fmt.Fprintln(os.Stderr)
		}
	}

	if *stats && len(items) > 1 {
		d, err := stdDeviation(items)
		if err != nil {
			log.Println("Error:", err)
		} else {
			fmt.Fprintf(os.Stderr, "Standard deviation: %d;%d;%d\n",
				d.Systolic, d.Diastolic, d.Pulse)
		}
		d, err = avgAbsoluteDeviation(items)
		if err != nil {
			log.Println("Error:", err)
		} else {
			fmt.Fprintf(os.Stderr, "Average absolute deviation: %d;%d;%d\n",
				d.Systolic, d.Diastolic, d.Pulse)
		}
	}
	if *stats && len(items) > 0 {
		m, err := median(items)
		if err != nil {
			log.Println("Error:", err)
		} else {
			fmt.Fprintf(os.Stderr, "Median values: %d;%d;%d",
				m.Systolic, m.Diastolic, m.Pulse)
			if *whoClass {
				fmt.Fprintf(os.Stderr, "  [%s]", m.WHOClassString())
			}
			fmt.Fprintln(os.Stderr)
		}

		if *whoClass {
			displayWHOClassStats(items)
		}
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
