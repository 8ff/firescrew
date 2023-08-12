package firescrewServe

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tj/go-naturaldate"
)

//go:embed static/*
var staticFiles embed.FS
var mediaPath string

type FileData struct {
	ID          string    `json:"ID"`
	MotionStart string    `json:"MotionStart"`
	MotionEnd   string    `json:"MotionEnd"`
	Objects     []Objects `json:"Objects"`
	Snapshots   []string  `json:"Snapshots"`
	VideoFile   string    `json:"VideoFile"`
	CameraName  string    `json:"CameraName"`
}

type Objects struct {
	BBox       BBox    `json:"BBox"`
	Center     Center  `json:"Center"`
	Area       int     `json:"Area"`
	LastMoved  string  `json:"LastMoved"`
	Class      string  `json:"Class"`
	Confidence float64 `json:"Confidence"`
}

type BBox struct {
	Min Coords `json:"Min"`
	Max Coords `json:"Max"`
}

type Coords struct {
	X int `json:"X"`
	Y int `json:"Y"`
}

type Center struct {
	X int `json:"X"`
	Y int `json:"Y"`
}

func Log(level, msg string) {
	switch level {
	case "info":
		fmt.Printf("\x1b[32m%s [INFO] %s\x1b[0m\n", time.Now().Format("15:04:05"), msg)
	case "error":
		fmt.Printf("\x1b[31m%s [ERROR] %s\x1b[0m\n", time.Now().Format("15:04:05"), msg)
	case "warning":
		fmt.Printf("\x1b[33m%s [WARNING] %s\x1b[0m\n", time.Now().Format("15:04:05"), msg)
	case "debug":
		fmt.Printf("\x1b[36m%s [DEBUG] %s\x1b[0m\n", time.Now().Format("15:04:05"), msg)
	default:
		fmt.Printf("%s [UNKNOWN] %s\n", time.Now().Format("15:04:05"), msg)
	}
}

func loadData(folder string) ([]FileData, error) {
	var data []FileData

	err := filepath.Walk(folder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && filepath.Ext(path) == ".json" {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			byteValue, err := io.ReadAll(file)
			if err != nil {
				return err
			}

			var fileData FileData

			if err = json.Unmarshal(byteValue, &fileData); err != nil {
				return fmt.Errorf("error parsing JSON from file %s: %w", path, err)
			}

			// Sort objects by FileData.MotionStart value
			sort.Slice(data, func(i, j int) bool {
				startI, err := time.Parse(time.RFC3339, data[i].MotionStart)
				if err != nil {
					log.Fatal(err)
				}
				startJ, err := time.Parse(time.RFC3339, data[j].MotionStart)
				if err != nil {
					log.Fatal(err)
				}
				return startI.After(startJ)
			})

			data = append(data, fileData)
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return data, nil
}

func ParseDateRangePrompt(prompt string) (time.Time, time.Time, error) {
	// Regular expression to match "from ... to ..." or "between ... and ..."
	re := regexp.MustCompile(`(?i)(from|between)\s+(.*?)\s+(to|and)\s+(.*)`)
	matches := re.FindStringSubmatch(prompt)
	if matches == nil {
		// Try to use basetime to atleast give full day range
		baseTime, err := naturaldate.Parse(prompt, time.Now())
		if err != nil {
			return time.Time{}, time.Time{}, err
		}

		// If baseTime hour/minute set to 00 then set tStart to 00:00:00 and tEnd to 23:59:59
		if baseTime.Hour() == 0 && baseTime.Minute() == 0 {
			tStart := time.Date(baseTime.Year(), baseTime.Month(), baseTime.Day(), 0, 0, 0, 0, baseTime.Location())
			tEnd := time.Date(baseTime.Year(), baseTime.Month(), baseTime.Day(), 23, 59, 59, 999999999, baseTime.Location())
			return tStart, tEnd, nil
		} else { // Use HH:MM from basetime as start time, endTime should be +1 hour
			tStart := time.Date(baseTime.Year(), baseTime.Month(), baseTime.Day(), baseTime.Hour(), baseTime.Minute(), 0, 0, baseTime.Location())
			tEnd := time.Date(baseTime.Year(), baseTime.Month(), baseTime.Day(), baseTime.Hour()+1, baseTime.Minute(), 0, 0, baseTime.Location())
			return tStart, tEnd, nil
		}
	}

	fmt.Printf("matches: %v\n", matches)

	// Extract the start and end time strings
	startStr := matches[2]
	endStr := matches[4]

	// fmt.Printf("startStr: %s\n", startStr)
	// fmt.Printf("endStr: %s\n", endStr)

	// Parse the start and end times
	tStart, err := naturaldate.Parse(startStr, time.Now())
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	tEnd, err := naturaldate.Parse(endStr, time.Now())
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	return tStart, tEnd, nil
}

func promptHandler(w http.ResponseWriter, r *http.Request) {
	type Tag struct {
		Tag  string `json:"tag"`
		Type string `json:"type"`
	}

	type retObj struct {
		Success   bool       `json:"success"`
		Error     string     `json:"error"`
		TimeStart string     `json:"timeStart"`
		TimeEnd   string     `json:"timeEnd"`
		Tags      []Tag      `json:"tags"`
		Data      []FileData `json:"data"`
	}

	prompt := r.URL.Query().Get("prompt")
	if prompt == "" {
		http.Error(w, "prompt parameter is required", http.StatusBadRequest)
		return
	}

	// Strip all punctuation
	prompt = strings.Map(func(r rune) rune {
		if strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 : ", r) {
			return r
		}
		return -1
	}, prompt)

	// fmt.Printf("prompt: %s\n", prompt)
	Log("info", fmt.Sprintf("Prompt: %s", prompt))

	data, err := loadData(mediaPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	Log("debug", fmt.Sprintf("Loaded %d files", len(data)))

	tStart, tEnd, err := ParseDateRangePrompt(prompt)
	if err != nil {
		Log("error", fmt.Sprintf("Error parsing date range prompt: %s", err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	Log("info", fmt.Sprintf("parsed start time: %v", tStart))
	Log("info", fmt.Sprintf("parsed end time: %v", tEnd))

	words := strings.Fields(prompt)
	var tags []Tag

	for _, word := range words {
		for _, fileData := range data {
			if word == fileData.CameraName {
				tags = append(tags, Tag{Tag: word, Type: "camera"})
				break
			}
		}
		for _, fileData := range data {
			for _, object := range fileData.Objects {
				singularWord := singular(word)
				if singularWord == object.Class {
					tags = append(tags, Tag{Tag: singularWord, Type: "class"})
					break
				}
			}
		}
	}

	uniqueTags := make(map[string]bool)
	var uniqueTagsList []Tag
	for _, tag := range tags {
		if !uniqueTags[tag.Tag] {
			uniqueTags[tag.Tag] = true
			uniqueTagsList = append(uniqueTagsList, tag)
		}
	}
	tags = uniqueTagsList

	fmt.Printf("tags: %v\n", tags)

	var filteredData []FileData
	for _, fileData := range data {
		motionStart, err := time.Parse(time.RFC3339, fileData.MotionStart)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if (motionStart.After(tStart) || motionStart.Equal(tStart)) && motionStart.Before(tEnd) {
			if len(tags) == 0 {
				filteredData = append(filteredData, fileData)
			} else {
				match := false
				for _, tag := range tags {
					if tag.Type == "camera" && fileData.CameraName == singular(tag.Tag) {
						match = true
						break
					}
					if tag.Type == "class" {
						for _, object := range fileData.Objects {
							if object.Class == singular(tag.Tag) {
								match = true
								break
							}
						}
					}
					if match {
						break
					}
				}
				if match {
					filteredData = append(filteredData, fileData)
				}
			}
		}
	}

	Log("info", fmt.Sprintf("Returning %d events", len(filteredData)))
	if filteredData == nil {
		filteredData = []FileData{}
	}

	ret := retObj{
		Success:   true,
		TimeStart: tStart.Format(time.RFC3339),
		TimeEnd:   tEnd.Format(time.RFC3339),
		Tags:      tags,
		Data:      filteredData,
	}

	json.NewEncoder(w).Encode(ret)
}

func singular(word string) string {
	irregularPlurals := map[string]string{
		"people": "person",
		"mice":   "mouse",
	}

	lowerWord := strings.ToLower(word)

	// Check if the word is in irregular plurals
	if singularWord, ok := irregularPlurals[lowerWord]; ok {
		return singularWord
	}

	// Else, just remove the "s" at the end if it's there
	wordLength := len(lowerWord)
	if lowerWord[wordLength-1] == 's' {
		return lowerWord[:wordLength-1]
	}

	// Return the word as it is if it's not plural
	return word
}

type httpRange struct {
	start, length int64
}

func (r *httpRange) contentRange(size int64) string {
	return fmt.Sprintf("bytes %d-%d/%d", r.start, r.start+r.length-1, size)
}

func rangeVideo(w http.ResponseWriter, req *http.Request) {

	// Get the requested file's path
	requestedFilePath := mediaPath + strings.TrimPrefix(req.URL.Path, "/rec/")

	// Check if .mp4 version of the file exists
	mp4FilePath := strings.TrimSuffix(requestedFilePath, filepath.Ext(requestedFilePath)) + ".mp4"
	if _, err := os.Stat(mp4FilePath); err == nil {
		// If it exists, serve the .mp4 version instead
		// Log("debug", fmt.Sprintf("Serving .mp4 file: %s", mp4FilePath))
		requestedFilePath = mp4FilePath // Replace with .mp4 file path
	}

	// Determine Content-Type based on file extension
	var contentType string
	switch filepath.Ext(requestedFilePath) {
	case ".ts":
		contentType = "video/MP2T"
	case ".mp4":
		contentType = "video/mp4"
	default:
		contentType = "application/octet-stream" // Fallback content type
	}

	// Open the requested file
	f, err := os.Open(requestedFilePath)
	if err != nil {
		Log("error", fmt.Sprintf("Unable to open video file: %s", err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	// Get the file size
	fi, err := f.Stat()
	if err != nil {
		Log("error", fmt.Sprintf("Unable to get file info: %s", err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	size := fi.Size()

	// Prepare to serve the entire file
	ra := &httpRange{
		start:  0,
		length: size,
	}

	// Check for a Range header in the request
	rangeHeader := req.Header.Get("Range")
	if rangeHeader != "" {
		// If a range is specified, parse it
		ranges := strings.Split(rangeHeader, "=")[1]
		rangesSplit := strings.Split(ranges, "-")
		start, err := strconv.ParseInt(rangesSplit[0], 10, 64)
		if err != nil {
			// log.Printf("Unable to parse range: %s", err)
			Log("error", fmt.Sprintf("Unable to parse range: %s", err))
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var end int64
		if rangesSplit[1] == "" {
			end = size - 1
		} else {
			end, err = strconv.ParseInt(rangesSplit[1], 10, 64)
			if err != nil {
				Log("error", fmt.Sprintf("Unable to parse range: %s", err))
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		// Update the range to serve
		ra.start = start
		ra.length = end - start + 1
	}

	// Set response headers
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(ra.length, 10))
	w.Header().Set("Content-Range", ra.contentRange(size))
	w.WriteHeader(http.StatusPartialContent)

	// Serve the specified range of the file
	if req.Method != http.MethodHead {
		f.Seek(ra.start, 0)
		io.CopyN(w, f, ra.length)
	}
}

// Serve images
func serveImages(w http.ResponseWriter, r *http.Request) {
	// Strip /images
	requestFile := strings.TrimPrefix(r.URL.Path, "/images/")
	img := mediaPath + requestFile
	file, err := os.Open(img)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "image/jpeg") //set the content type header to the appropriate image format
	_, err = io.Copy(w, file)                    // write the file to the response
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func Serve(path string, addr string) error {
	// If path doesnt end with / add it
	if path[len(path)-1:] != "/" {
		mediaPath = path + "/"
	} else {
		mediaPath = path
	}

	// Server images
	http.HandleFunc("/images/", serveImages)

	// Serve video files
	http.HandleFunc("/rec/", func(w http.ResponseWriter, r *http.Request) {
		rangeVideo(w, r)
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Open the file from the embedded filesystem
		f, err := staticFiles.Open("static/index.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer f.Close()

		// Write the contents of the file to the response
		_, err = io.Copy(w, f)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// Serve static files in static
	http.Handle("/static/", http.FileServer(http.FS(staticFiles)))

	// Serve API
	http.HandleFunc("/api", promptHandler)

	Log("info", fmt.Sprintf("Serving files from %s at %s", mediaPath, addr))

	err := http.ListenAndServe(addr, nil)
	if err != nil {
		return err
	}

	return nil
}

// func main() {
// 	Serve("../../rec/hi", ":8080")
// }
