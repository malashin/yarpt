package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/malashin/ffinfo"

	ansi "github.com/k0kubun/go-ansi"
	"golang.org/x/net/html/charset"
)

// apiURL and clientID for KinoPoisk api
// must be set here before compilation
var apiURL string
var clientID string
var fileListName = "fileList.txt"
var outputFileName = "report.txt"

// List of predetermined durations for the report.
var durationTypes = []int{90, 60, 30, 10, 5}

var regexpMap = map[string]*regexp.Regexp{
	"seCoid":           regexp.MustCompile(`.*?(?:s(\d{2})e(\d{2,4}))?(?:\_)?coid(\d+).*_r(\d+)x(\d+)p.*`),
	"durationHHMMSSMS": regexp.MustCompile(`.*Duration: (\d{2}\:\d{2}\:\d{2}\.\d{2}).*`),
}

type movie struct {
	Title         string `json:"title"`
	OriginalTitle string `json:"originalTitle"`
	Type          string `json:"type"`
}

// consolePrint prints str to console while cursor is hidden.
func consolePrint(str ...interface{}) {
	ansi.Print("\x1b[?25l") // Hide the cursor.
	ansi.Print(str...)
	ansi.Print("\x1b[?25h") // Show the cursor.
}

// readLines reads a whole file into memory and returns a slice of its lines.
func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func stripEscapesFromString(str string) string {
	return regexp.MustCompile(`(\x1b\[\d+m|\x1b\[\d+;\d+m)`).ReplaceAllString(str, "")
}

func writeStringToFile(file *os.File, input string, perm os.FileMode) {
	if _, err := file.WriteString(stripEscapesFromString(input)); err != nil {
		consolePrint("\x1b[31;1m", err, "\x1b[0m\n")
		os.Exit(1)
	}
}

// truncPad truncs or pads string to needed length.
// If side is 'r' the sring is padded and aligned to the right side.
// Otherwise it is aligned to the left side.
func truncPad(s string, n int, side byte) string {
	len := utf8.RuneCountInString(s)
	if len > n {
		return string([]rune(s)[0:n-3]) + "\x1b[30;1m...\x1b[0m"
	}
	if side == 'r' {
		return strings.Repeat(" ", n-len) + s
	}
	return s + strings.Repeat(" ", n-len)
}

// getMetaFromKP takes KinoPoisk ID and returns movies name in strings.
func getMetaFromKP(id string) (string, string, error) {
	if clientID == "" {
		return "", "", errors.New("clientID for KinoPoisk api is not provided")
	}

	req, err := http.NewRequest("GET", apiURL+id, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Clientid", clientID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	utf8, err := charset.NewReader(resp.Body, resp.Header.Get("Content-Type"))
	if err != nil {
		return "", "", fmt.Errorf("Encoding error: %v", err)
	}

	body, err := ioutil.ReadAll(utf8)
	if err != nil {
		return "", "", fmt.Errorf("IO error: %v", err)
	}

	m := movie{}
	err = json.Unmarshal(body, &m)
	if err != nil {
		return "", "", fmt.Errorf("json.Unmarshal error: %v", err)
	}

	movieName := ""
	if m.Title == "" {
		movieName = m.OriginalTitle // Use original title is Russian title is missing.
	} else {
		movieName = m.Title
	}
	if err != nil {
		return "", "", fmt.Errorf("TRANSLIT error: %v", err)
	}

	return movieName, m.Type, nil
}

// round rounds floats into integer numbers.
func round(input float64) int64 {
	if input < 0 {
		return int64(math.Ceil(input - 0.5))
	}
	return int64(math.Floor(input + 0.5))
}

// secondsToHHMMSS converts seconds (SS | SS.MS) to timecode (HH:MM:SS).
func secondsToHHMMSS(s float64) string {
	hh := math.Floor(s / 3600)
	mm := math.Floor((s - hh*3600) / 60)
	ss := int64(math.Floor(s-hh*3600-mm*60)) + round(math.Remainder(s, 1.0))

	hhString := strconv.FormatInt(int64(hh), 10)
	mmString := strconv.FormatInt(int64(mm), 10)
	ssString := strconv.FormatInt(int64(ss), 10)

	if hh < 10 {
		hhString = "0" + hhString
	}
	if mm < 10 {
		mmString = "0" + mmString
	}
	if ss < 10 {
		ssString = "0" + ssString
	}
	return hhString + ":" + mmString + ":" + ssString
}

func main() {
	// Read fileList and convert it into slice of strings.
	fileList, err := readLines(fileListName)
	if err != nil {
		consolePrint("\x1b[31;1m", err, "\x1b[0m\n")
		os.Exit(1)
	}
	fileListLength := len(fileList)
	if fileListLength < 1 {
		consolePrint("\x1b[31;1mERROR: \"" + fileListName + "\" is empty.\x1b[0m\n")
		os.Exit(1)
	}

	// Create empty output file.
	outputFile, err := os.Create(outputFileName)
	if err != nil {
		consolePrint("\x1b[31;1m", err, "\x1b[0m\n")
	}
	defer outputFile.Close()

	// For each file.
	for i, f := range fileList {
		// Get fileName from filePath.
		fileName := filepath.Base(f)

		// Check if file exists.
		if _, err := os.Stat(f); err != nil {
			consolePrint("\x1b[31;1m", f, ": No such file or directory.\x1b[0m\n")
			return
		}

		// Get KinoPoisk ID from fileName.
		season := regexpMap["seCoid"].ReplaceAllString(fileName, "${1}")
		episode := regexpMap["seCoid"].ReplaceAllString(fileName, "${2}")
		coid := regexpMap["seCoid"].ReplaceAllString(fileName, "${3}")
		rW, _ := strconv.Atoi(regexpMap["seCoid"].ReplaceAllString(fileName, "${4}"))
		rH, _ := strconv.Atoi(regexpMap["seCoid"].ReplaceAllString(fileName, "${5}"))
		if coid == fileName || coid == "" {
			consolePrint("\x1b[31;1m", "FileName is wrong.", "\x1b[0m\n")
			consolePrint("MUST BE: .*coid(\\d+).*\n\n")
			return
		}

		// Get movieName and movieType from KinoPisk API.
		movieName, movieType, err := getMetaFromKP(coid)
		if movieName == "" || movieType == "" {
			if err != nil {
				consolePrint("\x1b[31;1m", err, ".\x1b[0m\n")
			}
			consolePrint("\x1b[33;1m", "getMetaFromKP: Could not get data from KinoPoisk", "\x1b[0m\n")
			return
		}
		// Add season and episode numbers to movieName if movieType is SHOW.
		if movieType == "SHOW" {
			if season != "" || episode != "" {
				movieName = movieName + ". " + season + " сезон. " + episode + " серия"
			} else {
				movieName = movieName + ". ####"
			}
		}

		// Get file duration.
		file, err := ffinfo.Probe(f)
		if err != nil {
			consolePrint("\x1b[31;1m", "ffInfo: Could not get metadata from file", "\x1b[0m\n")
			return
		}
		durationString := file.Format.Duration
		duration, err := strconv.ParseFloat(durationString, 64)
		if err != nil {
			consolePrint("\x1b[31;1m", err, ".\x1b[0m\n")
			return
		}
		durationInMinutes := int(duration / 60)

		// The durations list must be sorted in decreasing order.
		sort.Sort(sort.Reverse(sort.IntSlice(durationTypes)))

		// Get file duration type according to durationTypes list.
		// 30 < x <= 60
		// x = 60
		durationInt := durationTypes[0]
		for _, d := range durationTypes[1:] {
			if durationInMinutes <= d {
				durationInt = d
			} else {
				break
			}
		}
		durationString = fmt.Sprintf("%02d", durationInt) + " минут"

		// Determine if resolution is SD or HD.
		resolution := "SD"
		if rW > 1024 || rH > 576 {
			resolution = "HD"
		}

		writeStringToFile(outputFile, movieName+"\t"+coid+"\t"+durationString+" "+resolution+"\t"+secondsToHHMMSS(duration)+"\t"+fileName+"\n", 0775)
		consolePrint(fmt.Sprintf("%"+strconv.Itoa(len(strconv.Itoa(fileListLength)))+"d", i+1) + "/" + strconv.Itoa(fileListLength) + "  " + truncPad(movieName, 32, 'l') + "  " + truncPad(coid, 8, 'l') + "  " + truncPad(durationString+" "+resolution, 12, 'l') + "  " + secondsToHHMMSS(duration) + "  " + truncPad(fileName, 32, 'l') + "\n")
	}
}
