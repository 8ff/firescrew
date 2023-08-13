package main

import (
	"bufio"
	"bytes"
	"embed"
	_ "net/http/pprof"

	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log"
	"math"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/8ff/firescrew/pkg/firescrewServe"
	"golang.org/x/image/bmp"

	"github.com/hybridgroup/mjpeg"
	"gocv.io/x/gocv"
)

var Version string

//go:embed assets/*
var assetsFs embed.FS

//go:embed models/mobilenetV1/ssd_mobilenet_v1_coco_2017_11_17.pbtxt
var embeddedModelConfig []byte

//go:embed models/mobilenetV1/ssd_mobilenet_v1_coco_2017_11_17/frozen_inference_graph.pb
var embeddedModelFile []byte

var stream *mjpeg.Stream

type Prediction struct {
	Object     int       `json:"object"`
	ClassName  string    `json:"class_name"`
	Box        []float32 `json:"box"`
	Top        int       `json:"top"`
	Bottom     int       `json:"bottom"`
	Left       int       `json:"left"`
	Right      int       `json:"right"`
	Confidence float32   `json:"confidence"`
}

type Config struct {
	CameraName                     string            `json:"cameraName"`
	PrintDebug                     bool              `json:"printDebug"`
	UseEmbeddedSSDMobileNetV1Model bool              `json:"useEmbeddedSSDMobileNetV1Model"`
	DeviceUrl                      string            `json:"deviceUrl"`
	HiResDeviceUrl                 string            `json:"hiResDeviceUrl"`
	ModelFile                      string            `json:"modelFile"`
	ModelConfig                    string            `json:"modelConfig"`
	PixelMotionAreaThreshold       float64           `json:"pixelMotionAreaThreshold"`
	ObjectCenterMovementThreshold  float64           `json:"objectCenterMovementThreshold"`
	ObjectAreaThreshold            float64           `json:"objectAreaThreshold"`
	StreamDrawIgnoredAreas         bool              `json:"streamDrawIgnoredAreas"`
	IgnoreAreasClasses             []IgnoreAreaClass `json:"ignoreAreasClasses"`
	EnableOutputStream             bool              `json:"enableOutputStream"`
	OutputStreamAddr               string            `json:"outputStreamAddr"`
	Motion                         struct {
		EmbeddedObjectDetector    bool     `json:"EmbeddedObjectDetector"`
		EmbeddedObjectScript      string   `json:"EmbeddedObjectScript"`
		ObjectMinThreshold        float64  `json:"objectMinThreshold"`
		LookForClasses            []string `json:"lookForClasses"`
		NetworkObjectDetectServer string   `json:"networkObjectDetectServer"`
		EventGap                  int      `json:"eventGap"`
		PrebufferSeconds          int      `json:"prebufferSeconds"`
	} `json:"motion"`
	Video struct {
		HiResPath     string `json:"hiResPath"`
		RecodeTsToMp4 bool   `json:"recodeTsToMp4"`
		LoResPath     string `json:"loResPath"`
	} `json:"video"`
}

// TODO ADD MUTEX LOCK
type Runtime struct {
	MotionTriggeredLast time.Time `json:"motionTriggeredLast"`
	MotionTriggered     bool      `json:"motionTriggered"`
	// MotionTriggeredChan chan bool `json:"motionTriggeredChan"`
	// MotionHiRecOn bool `json:"motionHiRecOn"`
	HiResControlChannel chan RecordMsg
	MotionVideo         VideoMetadata
	MotionMutex         *sync.Mutex
}

type IgnoreAreaClass struct {
	Class       []string `json:"class"`
	Coordinates string   `json:"coordinates"`
	Top         int      `json:"top"`
	Bottom      int      `json:"bottom"`
	Left        int      `json:"left"`
	Right       int      `json:"right"`
}

type ControlCommand struct {
	StartRecording bool
	Filename       string
}

type TrackedObject struct {
	BBox       image.Rectangle
	Center     image.Point
	Area       float64
	LastMoved  time.Time
	Class      string
	Confidence float32
}

type VideoMetadata struct {
	ID           string
	MotionStart  time.Time
	MotionEnd    time.Time
	Objects      []TrackedObject
	RecodedToMp4 bool
	Snapshots    []string
	VideoFile    string
	CameraName   string
}

var lastPositions = []TrackedObject{}
var globalConfig Config
var runtime Runtime

var predictFrameCounter int

type Frame struct {
	Data [][]byte
	Pts  time.Duration
}

func readConfig(path string) Config {
	// Read the configuration file.
	configFile, err := os.ReadFile(path)
	if err != nil {
		Log("error", fmt.Sprintf("Error reading config file: %v", err))
		os.Exit(1)
	}

	// Parse the configuration file into a Config struct.
	var config Config
	err = json.Unmarshal(configFile, &config)
	if err != nil {
		Log("error", fmt.Sprintf("Error parsing config file: %v", err))
		os.Exit(1)
	}

	// Split the coordinates string into separate integers.
	for i, ignoreAreaClass := range config.IgnoreAreasClasses {
		coords := strings.Split(ignoreAreaClass.Coordinates, ",")
		if len(coords) == 4 {
			config.IgnoreAreasClasses[i].Top, err = strconv.Atoi(coords[0])
			if err != nil {
				Log("error", fmt.Sprintf("Error parsing config file: %v", err))
				os.Exit(1)
			}
			config.IgnoreAreasClasses[i].Bottom, err = strconv.Atoi(coords[1])
			if err != nil {
				Log("error", fmt.Sprintf("Error parsing config file: %v", err))
				os.Exit(1)
			}
			config.IgnoreAreasClasses[i].Left, err = strconv.Atoi(coords[2])
			if err != nil {
				Log("error", fmt.Sprintf("Error parsing config file: %v", err))
				os.Exit(1)
			}
			config.IgnoreAreasClasses[i].Right, err = strconv.Atoi(coords[3])
			if err != nil {
				Log("error", fmt.Sprintf("Error parsing config file: %v", err))
				os.Exit(1)
			}
		} else {
			Log("error", fmt.Sprintf("Error parsing config file: %v", errors.New("coordinates string must contain 4 comma separated integers")))
			os.Exit(1)
		}
	}

	// Print the configuration properties.
	Log("info", "******************** CONFIG ********************")
	Log("info", fmt.Sprintf("Print Debug: %t", config.PrintDebug))
	Log("info", fmt.Sprintf("Use Embedded SSD MobileNetV1 Model: %t", config.UseEmbeddedSSDMobileNetV1Model))
	Log("info", fmt.Sprintf("Device URL: %s", config.DeviceUrl))
	Log("info", fmt.Sprintf("Hi-Res Device URL: %s", config.HiResDeviceUrl))
	Log("info", fmt.Sprintf("Model File: %s", config.ModelFile))
	Log("info", fmt.Sprintf("Model Config: %s", config.ModelConfig))
	Log("info", fmt.Sprintf("Video HiResPath: %s", config.Video.HiResPath))
	Log("info", fmt.Sprintf("Video LoResPath: %s", config.Video.LoResPath))
	Log("info", fmt.Sprintf("Video RecodeTsToMp4: %t", config.Video.RecodeTsToMp4))
	Log("info", fmt.Sprintf("Motion Embedded Object Detector: %t", config.Motion.EmbeddedObjectDetector))
	Log("info", fmt.Sprintf("Motion Object Min Threshold: %f", config.Motion.ObjectMinThreshold))
	Log("info", fmt.Sprintf("Motion LookForClasses: %v", config.Motion.LookForClasses))
	Log("info", fmt.Sprintf("Motion Network Object Detect Server: %s", config.Motion.NetworkObjectDetectServer))
	Log("info", fmt.Sprintf("Motion PrebufferSeconds: %d", config.Motion.PrebufferSeconds))
	Log("info", fmt.Sprintf("Motion EventGap: %d", config.Motion.EventGap))
	Log("info", fmt.Sprintf("Pixel Motion Area Threshold: %f", config.PixelMotionAreaThreshold))
	Log("info", fmt.Sprintf("Object Center Movement Threshold: %f", config.ObjectCenterMovementThreshold))
	Log("info", fmt.Sprintf("Object Area Threshold: %f", config.ObjectAreaThreshold))
	Log("info", "Ignore Areas Classes:")
	for _, ignoreAreaClass := range config.IgnoreAreasClasses {
		Log("info", fmt.Sprintf("  Class: %v, Coordinates: %s", ignoreAreaClass.Class, ignoreAreaClass.Coordinates))
	}
	Log("info", fmt.Sprintf("Draw Ignored Areas: %t", config.StreamDrawIgnoredAreas))
	Log("info", fmt.Sprintf("Enable Output Stream: %t", config.EnableOutputStream))
	Log("info", fmt.Sprintf("Output Stream Address: %s", config.OutputStreamAddr))
	Log("info", "************************************************")

	return config
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
		if globalConfig.PrintDebug {
			fmt.Printf("\x1b[36m%s [DEBUG] %s\x1b[0m\n", time.Now().Format("15:04:05"), msg)
		}
	default:
		fmt.Printf("%s [UNKNOWN] %s\n", time.Now().Format("15:04:05"), msg)
	}
}

type FrameMsg struct {
	Frame image.Image
	Error string
	// Exited   bool
	ExitCode int
}

type StreamInfo struct {
	Streams []struct {
		Width      int `json:"width"`
		Height     int `json:"height"`
		RFrameRate int `json:"-"`
	} `json:"streams"`
}

// RecordMsg struct to control recording
type RecordMsg struct {
	Record   bool
	Filename string
}

func getStreamInfo(rtspURL string) (StreamInfo, error) {
	cmd := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_streams", rtspURL)
	output, err := cmd.Output()
	if err != nil {
		return StreamInfo{}, err
	}

	// Unmarshal into a temporary structure to get the raw frame rate
	var rawInfo struct {
		Streams []struct {
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			RFrameRate string `json:"r_frame_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output, &rawInfo); err != nil {
		return StreamInfo{}, err
	}

	// Process the streams, converting the frame rate and filtering as needed
	var info StreamInfo
	for _, stream := range rawInfo.Streams {
		if stream.Width == 0 || stream.Height == 0 {
			continue // Skip streams with zero values
		}
		frParts := strings.Split(stream.RFrameRate, "/")
		if len(frParts) == 2 && frParts[1] == "1" {
			frameRate, err := strconv.Atoi(frParts[0])
			if err != nil {
				return StreamInfo{}, err
			}
			info.Streams = append(info.Streams, struct {
				Width      int `json:"width"`
				Height     int `json:"height"`
				RFrameRate int `json:"-"`
			}{
				Width:      stream.Width,
				Height:     stream.Height,
				RFrameRate: frameRate,
			})
		}
	}

	return info, nil
}

func CheckFFmpegAndFFprobe() (bool, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return false, fmt.Errorf("ffmpeg binary not found: %w", err)
	}

	if _, err := exec.LookPath("ffprobe"); err != nil {
		return false, fmt.Errorf("ffprobe binary not found: %w", err)
	}

	return true, nil
}

func processRTSPFeed(rtspURL string, msgChannel chan<- FrameMsg) {
	cmd := exec.Command("ffmpeg", "-rtsp_transport", "tcp", "-re", "-i", rtspURL, "-c:v", "bmp", "-f", "image2pipe", "-")

	stderrBuffer := &bytes.Buffer{}
	cmd.Stderr = stderrBuffer

	pipe, err := cmd.StdoutPipe()
	if err != nil {
		msgChannel <- FrameMsg{Error: err.Error()}
		return
	}

	err = cmd.Start()
	if err != nil {
		msgChannel <- FrameMsg{Error: err.Error()}
		return
	}

	reader := io.Reader(pipe)
	buffer := make([]byte, 14) // BMP header size

	for {
		_, err := io.ReadFull(reader, buffer)
		if err != nil {
			if err != io.EOF {
				msgChannel <- FrameMsg{Error: err.Error()}
			}
			break
		}

		if buffer[0] != 'B' || buffer[1] != 'M' {
			msgChannel <- FrameMsg{Error: "Not a BMP file"}
			continue
		}

		fileSize := binary.LittleEndian.Uint32(buffer[2:6])
		fileBuffer := make([]byte, fileSize-14)
		_, err = io.ReadFull(reader, fileBuffer)
		if err != nil {
			msgChannel <- FrameMsg{Error: err.Error()}
			continue
		}

		imageBuffer := append(buffer, fileBuffer...)
		img, err := bmp.Decode(bytes.NewReader(imageBuffer))
		if err != nil {
			msgChannel <- FrameMsg{Error: err.Error()}
			continue
		}

		msgChannel <- FrameMsg{Frame: img}
	}

	err = cmd.Wait()
	exitCode := 0 // Default exit code if no error occurred
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				exitCode = status.ExitStatus()
			}
		}
		msgChannel <- FrameMsg{Error: "FFmpeg exited with error: " + err.Error(), ExitCode: exitCode}
	}

	if stderrBuffer.Len() > 0 {
		msgChannel <- FrameMsg{Error: "FFmpeg STDERR: " + stderrBuffer.String()}
	}

	// Signal that the process has exited
	// msgChannel <- FrameMsg{Exited: true, ExitCode: exitCode}
}

func recordRTSPStream(rtspURL string, controlChannel <-chan RecordMsg, prebufferDuration time.Duration) {
	var file *os.File
	recording := false

	cmd := exec.Command("ffmpeg", "-rtsp_transport", "tcp", "-i", rtspURL, "-c", "copy", "-f", "mpegts", "pipe:1")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
		return
	}

	err = cmd.Start()
	if err != nil {
		log.Fatal(err)
		return
	}

	defer func() {
		if recording && file != nil {
			file.Close()
		}
		cmd.Wait()
	}()

	type chunkInfo struct {
		Data []byte
		Time time.Time
	}

	bufferSize := 4096
	prebuffer := make([]chunkInfo, 0)
	buffer := make([]byte, bufferSize)

	for {
		select {
		case msg := <-controlChannel:
			if msg.Record && !recording {
				file, err = os.Create(msg.Filename)
				if err != nil {
					log.Fatal(err)
					return
				}
				for _, chunk := range prebuffer { // Write prebuffered data
					_, err := file.Write(chunk.Data)
					if err != nil {
						log.Fatal(err)
						return
					}
				}
				recording = true
			} else if !msg.Record && recording {
				file.Close()
				recording = false
			}

		default:
			n, err := pipe.Read(buffer)
			if err != nil {
				if err == io.EOF {
					return
				}
				log.Fatal(err)
				return
			}

			// Prebuffer handling
			chunk := make([]byte, n)
			copy(chunk, buffer[:n])
			timestamp := time.Now()
			prebuffer = append(prebuffer, chunkInfo{Data: chunk, Time: timestamp})
			// Remove chunks that are older than prebufferDuration
			for len(prebuffer) > 1 && timestamp.Sub(prebuffer[0].Time) > prebufferDuration {
				prebuffer = prebuffer[1:]
			}

			if recording && file != nil {
				_, err := file.Write(buffer[:n])
				if err != nil {
					log.Fatal(err)
					return
				}
			}
		}
	}
}

func recodeToMP4(inputFile string) (string, error) {
	// Check if the input file has a .ts extension
	if !strings.HasSuffix(inputFile, ".ts") {
		return "", fmt.Errorf("input file must have a .ts extension")
	}

	// Remove the .ts extension and replace it with .mp4
	outputFile := strings.TrimSuffix(inputFile, ".ts") + ".mp4"

	// Create the FFmpeg command
	cmd := exec.Command("ffmpeg", "-i", inputFile, "-c:v", "libx264", "-c:a", "aac", outputFile)

	// Capture the standard output and standard error
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("FFmpeg command failed: %v\n%s", err, output)
	}

	return outputFile, nil
}

func main() {

	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	// Check if there is a config file argument, if there isnt give error and exit
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Not enough arguments provided\n")
		fmt.Println("Usage: firescrew [configfile]")
		fmt.Println("  -t, --template, t\tPrints the template config to stdout")
		fmt.Println("  -h, --help, h\t\tPrints this help message")
		fmt.Println("  -s, --serve, s\tStarts the web server, requires: [path] [addr]")
		return
	}

	switch os.Args[1] {
	case "-t", "--template", "t":
		// Dump template config to stdout
		printTemplateFile()
		return
	case "-h", "--help", "h":
		// Print help
		fmt.Println("Usage: firescrew [configfile]")
		fmt.Println("  -t, --template, t\tPrints the template config to stdout")
		fmt.Println("  -h, --help, h\t\tPrints this help message")
		fmt.Println("  -s, --serve, s\tStarts the web server, requires: [path] [addr]")
		return
	case "-s", "--serve", "s":
		// This requires 2 more params, a path to files and an addr in form :8080
		// Check if those params are provided if not give help message
		if len(os.Args) < 4 {
			fmt.Fprintf(os.Stderr, "Not enough arguments provided\n")
			fmt.Fprintf(os.Stderr, ("Usage: firescrew -s [path] [addr]\n"))
			return
		}
		firescrewServe.Serve(os.Args[2], os.Args[3])
	case "-v", "--version", "v":
		// Print version
		fmt.Println(Version)
		os.Exit(0)
	}

	// Read the config file
	globalConfig = readConfig(os.Args[1])

	// Check if ffmpeg/ffprobe binaries are available
	_, err := CheckFFmpegAndFFprobe()
	if err != nil {
		Log("error", "Unable to find ffmpeg/ffprobe binaries. Please install them")
		os.Exit(2)
	}

	// Print HI/LO stream details
	hiResStreamInfo, err := getStreamInfo(globalConfig.HiResDeviceUrl)
	if err != nil {
		Log("error", fmt.Sprintf("Error getting stream info: %v", err))
		return
	}
	loResStreamInfo, err := getStreamInfo(globalConfig.DeviceUrl)
	if err != nil {
		Log("error", fmt.Sprintf("Error getting stream info: %v", err))
		return
	}

	if len(hiResStreamInfo.Streams) == 0 {
		Log("error", fmt.Sprintf("No HI res streams found at %s", globalConfig.HiResDeviceUrl))
		return
	}

	if len(loResStreamInfo.Streams) == 0 {
		Log("error", fmt.Sprintf("No LO res streams found at %s", globalConfig.DeviceUrl))
		return
	}

	// Print stream info
	Log("info", "******************** STREAM INFO ********************")
	Log("info", fmt.Sprintf("Hi-Res Stream Resolution: %dx%d FPS: %d", hiResStreamInfo.Streams[0].Width, hiResStreamInfo.Streams[0].Height, hiResStreamInfo.Streams[0].RFrameRate))
	Log("info", fmt.Sprintf("Lo-Res Stream Resolution: %dx%d FPS: %d", loResStreamInfo.Streams[0].Width, loResStreamInfo.Streams[0].Height, loResStreamInfo.Streams[0].RFrameRate))
	Log("info", "*****************************************************")

	// If the user wants to use the embedded model, write the model to a file
	if globalConfig.UseEmbeddedSSDMobileNetV1Model {
		// Write the model config to a file
		err := storeModelFiles()
		if err != nil {
			Log("error", fmt.Sprintf("Error writing model files: %v", err))
			return
		}
	}

	// Define motion mutex
	runtime.MotionMutex = &sync.Mutex{}

	// If EmbeddedObjectDetector is set, run the embedded server
	if globalConfig.Motion.EmbeddedObjectDetector {
		// Copy assets to local filesystem
		path := copyAssetsToTemp()
		// Start the object detector
		go startObjectDetector(path + "/" + globalConfig.Motion.EmbeddedObjectScript)

		// Set networkObjectDetectServer path to 127.0.0.1:8555
		globalConfig.Motion.NetworkObjectDetectServer = "127.0.0.1:8555"

		time.Sleep(5 * time.Second)
	}

	backend := gocv.NetBackendDefault
	backend = gocv.ParseNetBackend(globalConfig.ModelFile)

	stream = mjpeg.NewStream()
	if globalConfig.EnableOutputStream {
		go startWebcamStream(stream)
	}

	// Prepare motion
	img := gocv.NewMat()
	defer img.Close()

	// open DNN object tracking model
	net := gocv.ReadNet(globalConfig.ModelFile, globalConfig.ModelConfig)
	if net.Empty() {
		fmt.Printf("Error reading network model from : %v %v\n", globalConfig.ModelFile, globalConfig.ModelConfig)
		return
	}
	defer net.Close()
	net.SetPreferableBackend(gocv.NetBackendType(backend))
	net.SetPreferableTarget(gocv.NetTargetType(gocv.ParseNetTarget(globalConfig.ModelConfig)))

	var ratio float64
	var mean gocv.Scalar
	var swapRGB bool

	if filepath.Ext(globalConfig.ModelFile) == ".caffemodel" {
		ratio = 1.0
		mean = gocv.NewScalar(104, 177, 123, 0)
		swapRGB = false
	} else {
		ratio = 1.0 / 127.5
		mean = gocv.NewScalar(127.5, 127.5, 127.5, 0)
		swapRGB = true
	}

	// Prepare motion detection
	imgDelta := gocv.NewMat()
	defer imgDelta.Close()

	imgThresh := gocv.NewMat()
	defer imgThresh.Close()

	mog2 := gocv.NewBackgroundSubtractorMOG2()
	defer mog2.Close()

	// Start HI Res prebuffering
	runtime.HiResControlChannel = make(chan RecordMsg)
	go func() {
		recordRTSPStream(globalConfig.HiResDeviceUrl, runtime.HiResControlChannel, time.Duration(globalConfig.Motion.PrebufferSeconds)*time.Second)
		defer close(runtime.HiResControlChannel)
		time.Sleep(5 * time.Second)
		Log("warning", "Restarting HI RTSP feed")
	}()

	frameChannel := make(chan FrameMsg)
	go func(frameChannel chan FrameMsg) {
		for {
			processRTSPFeed(globalConfig.DeviceUrl, frameChannel)
			Log("warning", "EXITED")
			//*********** EXITS BELOW ***********//
			time.Sleep(5 * time.Second)
			Log("warning", "Restarting LO RTSP feed")
		}
	}(frameChannel)

	for msg := range frameChannel {
		if msg.Error != "" {
			Log("error", msg.Error)
			continue
		}

		if msg.Frame != nil {
			img, err := gocv.ImageToMatRGB(msg.Frame)
			if err != nil {
				Log("error", fmt.Sprintf("Conversion Error: %s", err))
				continue
			}

			if img.Empty() {
				continue
			}

			buf, _ := gocv.IMEncode(".jpg", img)
			stream.UpdateJPEG(buf.GetBytes())
			buf.Close()

			// Handle all motion stuff here
			if isMotion(img, mog2, imgDelta, imgThresh, 25, globalConfig.PixelMotionAreaThreshold) {
				// If its been more than globalConfig.Motion.EventGap seconds since the last motion event, untrigger
				if runtime.MotionTriggered && time.Since(runtime.MotionTriggeredLast) > time.Duration(globalConfig.Motion.EventGap)*time.Second {
					Log("info", fmt.Sprintf("SINCE_LAST_EVENT: %d GAP: %d", time.Since(runtime.MotionTriggeredLast), time.Duration(globalConfig.Motion.EventGap)*time.Second))
					Log("info", "MOTION_ENDED")
					runtime.MotionMutex.Lock()
					// Stop Hi res recording and dump json file as well as clear struct
					runtime.MotionVideo.MotionEnd = time.Now()
					runtime.HiResControlChannel <- RecordMsg{Record: false}

					if globalConfig.Video.RecodeTsToMp4 { // Store this for future reference
						runtime.MotionVideo.RecodedToMp4 = true
						go func(videoFile string) {
							// Recode the ts file to mp4
							_, err := recodeToMP4(videoFile)
							if err != nil {
								Log("error", fmt.Sprintf("Error recoding ts file to mp4: %v", err))
							} else {
								// Remove the ts file
								err = os.Remove(videoFile)
								if err != nil {
									Log("error", fmt.Sprintf("Error removing ts file: %v", err))
								}
							}
						}(filepath.Join(globalConfig.Video.HiResPath, runtime.MotionVideo.VideoFile))
					}

					jsonData, err := json.Marshal(runtime.MotionVideo)
					if err != nil {
						Log("error", fmt.Sprintf("Error marshalling metadata: %v", err))
					}

					err = os.WriteFile(filepath.Join(globalConfig.Video.HiResPath, fmt.Sprintf("meta_%s.json", runtime.MotionVideo.ID)), jsonData, 0644)
					if err != nil {
						Log("error", fmt.Sprintf("Error writing metadata file: %v", err))
					}

					// 	// Clear the whole runtime.MotionVideo struct
					runtime.MotionVideo = VideoMetadata{}

					runtime.MotionTriggered = false
					runtime.MotionMutex.Unlock()
				}

				if globalConfig.Motion.NetworkObjectDetectServer != "" {
					// Python motion detection
					if predictFrameCounter%5 == 0 {
						if predictFrameCounter > 10000 {
							predictFrameCounter = 0
						}
						// Only run this on every 5th frame
						if msg.Frame != nil {
							// Send data to yoloPredict
							predict, err := yoloPredict(msg.Frame)
							if err != nil {
								Log("error", fmt.Sprintf("Error running yoloPredict: %v", err))
								return
							}

							// fmt.Printf("predict: %v\n", predict)
							performDetectionOnObject(&img, predict) // Perform the detection
						}
					}
					predictFrameCounter++
				} else {

					///////// LOCAL OBJECT DETECT /////////
					// convert image Mat to 300x300 blob that the object detector can analyze
					// blob := gocv.BlobFromImage(img, ratio, image.Pt(img.Cols(), img.Rows()), mean, swapRGB, false)
					blob := gocv.BlobFromImage(img, ratio, image.Pt(300, 300), mean, swapRGB, false)

					// feed the blob into the detector
					net.SetInput(blob, "")

					// run a forward pass thru the network
					prob := net.Forward("")
					performDetection(&img, prob) // Perform the detection
					prob.Close()
					blob.Close()

					// Copy &img to gocv.Mat to keep the object detection rectangle
					qimg := gocv.NewMat()
					img.CopyTo(&qimg)
					// defer qimg.Close()
					///////////// END LOCAL OBJECT DETECT /////////////
				}

				// writer.Write(qimg) // Write the frame to file

				if globalConfig.EnableOutputStream {
					streamImage(&img, stream) // Stream the image to the web
				}
			} else {
				if globalConfig.EnableOutputStream {
					streamImage(&img, stream) // Stream the image to the web
				}
			}

			// Cleanup
			img.Close()
		}
	}

	// Start HI Res recording
	// go func() {
	// 	_, err = streamHandleHi.Client.Play(nil)
	// 	if err != nil {
	// 		panic(err)
	// 	}

	// 	// wait until a fatal error
	// 	panic(streamHandleHi.Client.Wait())
	// }()

	// start playing
	// _, err = streamHandleLo.Client.Play(nil)
	// if err != nil {
	// 	panic(err)
	// }

	// wait until a fatal error
	// panic(streamHandleLo.Client.Wait())
}

func isMotion(img gocv.Mat, mog2 gocv.BackgroundSubtractorMOG2, imgDelta gocv.Mat, imgThresh gocv.Mat, fps float64, MinimumArea float64) bool {
	// Motion detection
	// first phase of cleaning up image, obtain foreground only
	mog2.Apply(img, &imgDelta)

	// remaining cleanup of the image to use for finding contours.
	// first use threshold
	gocv.Threshold(imgDelta, &imgThresh, float32(fps), 255, gocv.ThresholdBinary)

	// then dilate
	kernel := gocv.GetStructuringElement(gocv.MorphRect, image.Pt(3, 3))
	gocv.Dilate(imgThresh, &imgThresh, kernel)
	kernel.Close()

	// now find contours
	contours := gocv.FindContours(imgThresh, gocv.RetrievalExternal, gocv.ChainApproxSimple)

	defer contours.Close()

	for i := 0; i < contours.Size(); i++ {
		area := gocv.ContourArea(contours.At(i))
		if area < MinimumArea {
			continue
		} else {
			return true
		}
	}

	return false
}

// performDetection analyzes the results from the detector network,
// which produces an output blob with a shape 1x1xNx7
// where N is the number of detections, and each detection
// is a vector of float values
// [batchId, classId, confidence, left, top, right, bottom]
func performDetection(frame *gocv.Mat, results gocv.Mat) {
	now := time.Now()

	for i := 0; i < results.Total(); i += 7 {
		confidence := results.GetFloatAt(0, i+2)
		classId := int(results.GetFloatAt(0, i+1))
		if confidence > float32(globalConfig.Motion.ObjectMinThreshold) {
			// fmt.Printf("classID: %s, confidence: %v\n", getClass(int(classId)), confidence)
			left := int(results.GetFloatAt(0, i+3) * float32(frame.Cols()))
			top := int(results.GetFloatAt(0, i+4) * float32(frame.Rows()))
			right := int(results.GetFloatAt(0, i+5) * float32(frame.Cols()))
			bottom := int(results.GetFloatAt(0, i+6) * float32(frame.Rows()))

			rect := image.Rect(left, top, right, bottom)

			object := TrackedObject{
				BBox:       rect,
				Center:     image.Pt((left+right)/2, (top+bottom)/2),
				LastMoved:  now,
				Area:       float64(rect.Dx() * rect.Dy()),
				Class:      getClass(int(classId)),
				Confidence: confidence,
			}

			// If object is not within LookForClasses, skip it
			if len(globalConfig.Motion.LookForClasses) > 0 {
				found := false
				for _, filterClass := range globalConfig.Motion.LookForClasses {
					if object.Class == filterClass {
						found = true
					}
				}
				if !found {
					continue
				}
			}

			exists := findObjectPosition(object)
			if !exists {

				// Check if this object is within the areas of interest
				for _, ignoreAreaClass := range globalConfig.IgnoreAreasClasses {
					for _, class := range ignoreAreaClass.Class {
						if class == object.Class {
							if object.Center.X > ignoreAreaClass.Left && object.Center.X < ignoreAreaClass.Right && object.Center.Y > ignoreAreaClass.Top && object.Center.Y < ignoreAreaClass.Bottom {
								// This object is within an ignore area, skip it
								// fmt.Printf("Ignoring object %s @ %d|%f\n", object, object.Center)
								// Log("warning", fmt.Sprintf("IGNORING OBJECT @ %d|%f [%s|%f]", object.Center, object.Area, object.Class, object.Confidence))
								return
							}
						}
					}
				}

				Log("info", fmt.Sprintf("TRIGGERED NEW OBJECT @ %d|%f [%s|%f]", object.Center, object.Area, object.Class, object.Confidence))
				if !runtime.MotionTriggered { // If this is NEW motion
					// Lock mutex
					runtime.MotionMutex.Lock()
					runtime.MotionTriggered = true
					runtime.MotionTriggeredLast = now
					runtime.MotionVideo.CameraName = globalConfig.CameraName
					runtime.MotionVideo.MotionStart = now
					// Generate random string filename for runtime.MotionVideo.Filename
					runtime.MotionVideo.ID = generateRandomString(15) + ".ts"
					runtime.MotionVideo.Objects = append(runtime.MotionVideo.Objects, object)
					runtime.MotionVideo.VideoFile = fmt.Sprintf("clip_%s.ts", runtime.MotionVideo.ID)                                                            // Set filename for video file
					runtime.HiResControlChannel <- RecordMsg{Record: true, Filename: filepath.Join(globalConfig.Video.HiResPath, runtime.MotionVideo.VideoFile)} // Start recording

					// Unlock mutex
					runtime.MotionMutex.Unlock()
				} else { // If this is EXISTING motion
					// Lock mutex
					runtime.MotionMutex.Lock()
					runtime.MotionTriggeredLast = now
					runtime.MotionVideo.Objects = append(runtime.MotionVideo.Objects, object)
					// Unlock mutex
					runtime.MotionMutex.Unlock()
				}

				Log("error", fmt.Sprintf("STORED %d OBJECTS", len(runtime.MotionVideo.Objects)))

				gocv.Rectangle(frame, rect, color.RGBA{0, 255, 0, 0}, 2)

				// Adding the label
				pt := image.Pt(left, top-5)
				if top-5 < 0 {
					pt = image.Pt(left, top+20) // if the box is too close to the top of the image, put the label inside the box
				}
				gocv.PutText(frame, fmt.Sprintf("%s %.2f", getClass(int(classId)), confidence), pt, gocv.FontHersheyPlain, 2.2, color.RGBA{0, 255, 0, 0}, 2)

				// Store snapshot of the object
				if runtime.MotionVideo.ID != "" {
					snapshotFilename := fmt.Sprintf("snap_%s_%s.jpg", runtime.MotionVideo.ID, generateRandomString(4))
					runtime.MotionVideo.Snapshots = append(runtime.MotionVideo.Snapshots, snapshotFilename)
					gocv.IMWrite(fmt.Sprintf("%s/%s", globalConfig.Video.HiResPath, snapshotFilename), *frame)
				} else {
					Log("warning", "runtime.MotionVideo.ID is empty, not writing snapshot. This shouldnt happen.")
				}
			}
		}
	}
}

func performDetectionOnObject(frame *gocv.Mat, prediction []Prediction) {
	now := time.Now()

	for _, predict := range prediction {
		// If class is not within LookForClasses, skip it
		if len(globalConfig.Motion.LookForClasses) > 0 {
			found := false
			for _, filterClass := range globalConfig.Motion.LookForClasses {
				if predict.ClassName == filterClass {
					found = true
				}
			}
			if !found {
				continue
			}
		}

		if predict.Confidence < float32(globalConfig.Motion.ObjectMinThreshold) {
			continue
		}

		rect := image.Rect(predict.Left, predict.Top, predict.Right, predict.Bottom)

		object := TrackedObject{
			BBox:       rect,
			Center:     image.Pt((predict.Left+predict.Right)/2, (predict.Top+predict.Bottom)/2),
			LastMoved:  now,
			Area:       float64(rect.Dx() * rect.Dy()),
			Class:      predict.ClassName,
			Confidence: predict.Confidence,
		}

		exists := findObjectPosition(object)
		if !exists {

			// Check if this object is within the areas of interest
			for _, ignoreAreaClass := range globalConfig.IgnoreAreasClasses {
				for _, class := range ignoreAreaClass.Class {
					if class == object.Class {
						if object.Center.X > ignoreAreaClass.Left && object.Center.X < ignoreAreaClass.Right && object.Center.Y > ignoreAreaClass.Top && object.Center.Y < ignoreAreaClass.Bottom {
							// This object is within an ignore area, skip it
							// fmt.Printf("Ignoring object %s @ %d|%f\n", object, object.Center)
							// Log("warning", fmt.Sprintf("IGNORING OBJECT @ %d|%f [%s|%f]", object.Center, object.Area, object.Class, object.Confidence))
							return
						}
					}
				}
			}

			Log("info", fmt.Sprintf("TRIGGERED NEW OBJECT @ %d|%f [%s|%f]", object.Center, object.Area, object.Class, object.Confidence))
			if !runtime.MotionTriggered {
				// Lock mutex
				runtime.MotionMutex.Lock()
				runtime.MotionTriggered = true
				runtime.MotionTriggeredLast = now
				runtime.MotionVideo.CameraName = globalConfig.CameraName
				runtime.MotionVideo.MotionStart = now
				// Generate random string filename for runtime.MotionVideo.Filename
				runtime.MotionVideo.ID = generateRandomString(15)
				runtime.MotionVideo.Objects = append(runtime.MotionVideo.Objects, object)
				runtime.MotionVideo.VideoFile = fmt.Sprintf("clip_%s.ts", runtime.MotionVideo.ID)                                                            // Set filename for video file
				runtime.HiResControlChannel <- RecordMsg{Record: true, Filename: filepath.Join(globalConfig.Video.HiResPath, runtime.MotionVideo.VideoFile)} // Start recording

				// Unlock mutex
				runtime.MotionMutex.Unlock()
			} else {
				// Lock mutex
				runtime.MotionMutex.Lock()
				runtime.MotionTriggeredLast = now
				runtime.MotionVideo.Objects = append(runtime.MotionVideo.Objects, object)
				// Unlock mutex
				runtime.MotionMutex.Unlock()
			}

			Log("error", fmt.Sprintf("STORED %d OBJECTS", len(runtime.MotionVideo.Objects)))

			gocv.Rectangle(frame, rect, color.RGBA{0, 255, 0, 0}, 2)

			// Adding the label
			pt := image.Pt(predict.Left, predict.Top-5)
			if predict.Top-5 < 0 {
				pt = image.Pt(predict.Left, predict.Top+20) // if the box is too close to the top of the image, put the label inside the box
			}
			gocv.PutText(frame, fmt.Sprintf("%s %.2f", predict.ClassName, predict.Confidence), pt, gocv.FontHersheyPlain, 2.2, color.RGBA{0, 255, 0, 0}, 2)

			// Store snapshot of the object
			if runtime.MotionVideo.ID != "" {
				snapshotFilename := fmt.Sprintf("snap_%s_%s.jpg", runtime.MotionVideo.ID, generateRandomString(4))
				runtime.MotionVideo.Snapshots = append(runtime.MotionVideo.Snapshots, snapshotFilename)
				gocv.IMWrite(fmt.Sprintf("%s/%s", globalConfig.Video.HiResPath, snapshotFilename), *frame)
			} else {
				Log("warning", "runtime.MotionVideo.ID is empty, not writing snapshot. This shouldnt happen.")
			}
		}
	}

}

// Function that goes over lastPositions and checks if any of them are within of a threshold of the current center
func findObjectPosition(object TrackedObject) bool {
	// Check if this object has been seen before
	for i := 0; i < len(lastPositions); i++ {
		distance := math.Sqrt(float64((object.Center.X-lastPositions[i].Center.X)*(object.Center.X-lastPositions[i].Center.X) + (object.Center.Y-lastPositions[i].Center.Y)*(object.Center.Y-lastPositions[i].Center.Y)))
		// fmt.Printf("Distance: %v\n", distance)
		if distance < globalConfig.ObjectCenterMovementThreshold {
			// Compare area as well to see if its +- within the threshold
			areaDiff := math.Abs(object.Area - lastPositions[i].Area)
			// fmt.Printf("Area diff: %v\n", areaDiff)
			if areaDiff < globalConfig.ObjectAreaThreshold {
				// This means a match, overwrite old object with updated one
				// Log("warning", fmt.Sprintf("UPDATING OBJECT @ %d|%f TO %d|%f DISTANCE: %d ADIFF: %d", lastPositions[i].Center, lastPositions[i].Area, object.Center, object.Area, int(distance), int(areaDiff)))
				lastPositions[i] = object
				return true
			}
		}
	}

	// Clean up lastPositions
	for i := 0; i < len(lastPositions); i++ {
		// Check if last update is more than 30 seconds ago
		if time.Since(lastPositions[i].LastMoved) > 30*time.Second {
			// Delete this object
			// Log("error", fmt.Sprintf("EXPIRING OBJECT @ %d|%f [%s|%f]", lastPositions[i].Center, lastPositions[i].Area, lastPositions[i].Class, lastPositions[i].Confidence))
			lastPositions = append(lastPositions[:i], lastPositions[i+1:]...)
		}
	}

	// This is a new object, add it
	lastPositions = append(lastPositions, object)
	return false
}

func streamImage(img *gocv.Mat, stream *mjpeg.Stream) {
	// Draw ignore areas from IgnoreAreasClasses
	if globalConfig.StreamDrawIgnoredAreas {
		for _, ignoreAreaClass := range globalConfig.IgnoreAreasClasses {
			// Draw the ignore area
			rect := image.Rect(ignoreAreaClass.Left, ignoreAreaClass.Top, ignoreAreaClass.Right, ignoreAreaClass.Bottom)
			gocv.Rectangle(img, rect, color.RGBA{255, 0, 0, 0}, 2)
		}
	}

	// Stream video over http
	buf, _ := gocv.IMEncode(".jpg", *img) // dereference the pointer to get the gocv.Mat value
	stream.UpdateJPEG(buf.GetBytes())
	buf.Close()
}

func startWebcamStream(stream *mjpeg.Stream) {
	// start http server
	http.Handle("/", stream)

	server := &http.Server{
		Addr:         globalConfig.OutputStreamAddr,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	log.Fatal(server.ListenAndServe())
}

func getClass(id int) string {
	classes := map[int]string{
		1:  "person",
		2:  "bicycle",
		3:  "car",
		4:  "motorcycle",
		5:  "airplane",
		6:  "bus",
		7:  "train",
		8:  "truck",
		9:  "boat",
		10: "traffic light",
		11: "fire hydrant",
		13: "stop sign",
		14: "parking meter",
		15: "bench",
		16: "bird",
		17: "cat",
		18: "dog",
		19: "horse",
		20: "sheep",
		21: "cow",
		22: "elephant",
		23: "bear",
		24: "zebra",
		25: "giraffe",
		27: "backpack",
		28: "umbrella",
		31: "handbag",
		32: "tie",
		33: "suitcase",
		34: "frisbee",
		35: "skis",
		36: "snowboard",
		37: "sports ball",
		38: "kite",
		39: "baseball bat",
		40: "baseball glove",
		41: "skateboard",
		42: "surfboard",
		43: "tennis racket",
		44: "bottle",
		46: "wine glass",
		47: "cup",
		48: "fork",
		49: "knife",
		50: "spoon",
		51: "bowl",
		52: "banana",
		53: "apple",
		54: "sandwich",
		55: "orange",
		56: "broccoli",
		57: "carrot",
		58: "hot dog",
		59: "pizza",
		60: "donut",
		61: "cake",
		62: "chair",
		63: "couch",
		64: "potted plant",
		65: "bed",
		67: "dining table",
		70: "toilet",
		72: "tv",
		73: "laptop",
		74: "mouse",
		75: "remote",
		76: "keyboard",
		77: "cell phone",
		78: "microwave",
		79: "oven",
		80: "toaster",
		81: "sink",
		82: "refrigerator",
		84: "book",
		85: "clock",
		86: "vase",
		87: "scissors",
		88: "teddy bear",
		89: "hair drier",
		90: "toothbrush",
	}

	return classes[id]
}

func yoloPredict(imgRaw image.Image) ([]Prediction, error) {
	// Create a channel to communicate the result of the function
	resultChan := make(chan []Prediction, 1)
	errorChan := make(chan error, 1)

	// Start the actual work in a goroutine
	go func() {
		// Start timer
		// start := time.Now()

		// Convert the image to a byte array
		buf := new(bytes.Buffer)
		if err := jpeg.Encode(buf, imgRaw, nil); err != nil {
			errorChan <- err
			return
		}
		imgData := buf.Bytes()

		// Dial a TCP connection with context
		d := net.Dialer{}
		conn, err := d.Dial("tcp", globalConfig.Motion.NetworkObjectDetectServer)
		if err != nil {
			errorChan <- err
			return
		}
		defer conn.Close()

		// Send the size of the image data
		size := uint32(len(imgData))
		sizeBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(sizeBytes, size)
		if _, err := conn.Write(sizeBytes); err != nil {
			errorChan <- err
			return
		}

		// Send the image data
		if _, err := conn.Write(imgData); err != nil {
			errorChan <- err
			return
		}

		// Read the response
		reader := bufio.NewReader(conn)
		respData, err := reader.ReadBytes('\n')
		if err != nil {
			errorChan <- err
			return
		}

		// Parse the response data as a Prediction
		var preds []Prediction
		if err := json.Unmarshal(respData, &preds); err != nil {
			errorChan <- err
			return
		}

		for i, pred := range preds {
			box := pred.Box
			preds[i].Top = int(box[1])
			preds[i].Bottom = int(box[3])
			preds[i].Left = int(box[0])
			preds[i].Right = int(box[2])
		}

		resultChan <- preds
		// Log("info", fmt.Sprintf("TOOK: %v", time.Since(start)))
	}()

	// Wait for the result or the context timeout
	select {
	case result := <-resultChan:
		return result, nil
	case err := <-errorChan:
		return nil, err
	case <-time.After(3 * time.Second):
		return nil, errors.New("operation timed out")
	}
}

func startObjectDetector(scriptPath string) {
	basePath := filepath.Dir(scriptPath)
	restartCount := 0
	pidFileName := filepath.Base(scriptPath) + ".pid"
	pidFilePath := filepath.Join("/tmp", pidFileName)

	// Try to read existing PID file
	pidData, err := os.ReadFile(pidFilePath)
	if err == nil {
		pid, err := strconv.Atoi(string(pidData))
		if err == nil {
			process, err := os.FindProcess(pid)
			if err == nil {
				process.Kill() // Try to kill the existing process
			}
		}
	}

	for {
		if restartCount > 3 {
			Log("error", "Embedded python script failed 3 times, giving up")
			os.Exit(1)
		}

		cmd := exec.Command("python3", scriptPath)
		cmd.Dir = basePath

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			Log("error", fmt.Sprintf("Error creating StdoutPipe for Cmd: %v", err))
			continue
		}

		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		Log("info", "Starting embedded python object server")
		err = cmd.Start()

		if err != nil {
			Log("error", fmt.Sprintf("Error starting Cmd: %v", err))
			continue
		}

		// Write PID to file
		err = os.WriteFile(pidFilePath, []byte(strconv.Itoa(cmd.Process.Pid)), 0644)
		if err != nil {
			Log("error", fmt.Sprintf("Error writing PID to file: %v", err))
			cmd.Process.Kill()
			continue
		}

		go readOutput(stdout)

		// Create a channel to catch signals to the process
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-c
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
			os.Remove(pidFilePath) // Remove PID file
			os.Exit(1)
		}()

		err = cmd.Wait()

		if err != nil {
			Log("error", fmt.Sprintf("Embedded python script failed: %s", stderr.String()))
		} else {
			Log("info", "Embedded python script exited, restarting...")
		}

		os.Remove(pidFilePath) // Remove PID file

		time.Sleep(2 * time.Second)
		restartCount++
	}
}

// Helped function for startObjectDetector
func readOutput(r io.ReadCloser) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		// Strip newlines
		out := strings.TrimSuffix(scanner.Text(), "\n")

		// Strip empty lines
		if out == "" {
			continue
		}

		Log("debug", out)
	}
}

// Function that copies assetsFs to /tmp in a random folder and returns path
func copyAssetsToTemp() string {
	// Create a random folder in /tmp
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	tempDir := fmt.Sprintf("/tmp/%d", r.Intn(1000)+1)
	os.Mkdir(tempDir, 0755)

	// Delete the temporary directory when the application exits
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		os.RemoveAll(tempDir)
		os.Exit(0)
	}()

	// Copy assets to temp dir
	assets, err := assetsFs.ReadDir("assets") // Read the "assets" directory instead of "."
	if err != nil {
		panic(err)
	}

	for _, asset := range assets {
		// fmt.Printf("ASSET: %s\n", asset.Name())

		if asset.IsDir() {
			continue
		}
		data, err := assetsFs.ReadFile("assets/" + asset.Name()) // Add "assets/" prefix when reading the file
		if err != nil {
			panic(err)
		}

		err = os.WriteFile(filepath.Join(tempDir, asset.Name()), data, 0644)
		if err != nil {
			panic(err)
		}

		// Make *.py files executable
		if filepath.Ext(asset.Name()) == ".py" {
			err = os.Chmod(filepath.Join(tempDir, asset.Name()), 0755)
			if err != nil {
				panic(err)
			}
		}
	}

	return tempDir
}

// Function that stores embedded model files in a temp directory and sets globalConfig.ModelFile and globalConfig.ModelConfig to proper paths
func storeModelFiles() error {
	tmpDir, err := os.MkdirTemp("", "models")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %v", err)
	}

	modelFile := filepath.Join(tmpDir, "model.pb")
	err = os.WriteFile(modelFile, embeddedModelFile, 0644)
	if err != nil {
		return fmt.Errorf("failed to write model file: %v", err)
	}

	modelConfig := filepath.Join(tmpDir, "modelConfig.pbtxt")
	err = os.WriteFile(modelConfig, embeddedModelConfig, 0644)
	if err != nil {
		return fmt.Errorf("failed to write model config: %v", err)
	}

	globalConfig.ModelFile = modelFile
	globalConfig.ModelConfig = modelConfig

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		os.RemoveAll(tmpDir)
		os.Exit(0)
	}()

	return nil
}

// This function prints out embedded template.json file
func printTemplateFile() {
	fileBytes, err := assetsFs.ReadFile("assets/template.json")
	if err != nil {
		log.Fatalf("Failed to read template file: %v", err)
	}

	fmt.Println(string(fileBytes))
}

func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz" + "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	seededRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

// Function that starts the webserver on port 4534
