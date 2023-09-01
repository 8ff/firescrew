package objectPredict

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"embed"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	onnx "github.com/8ff/onnxruntime_go"
	"github.com/goki/freetype"
	"golang.org/x/image/draw"
	"golang.org/x/image/math/fixed"
)

//go:embed models
var models embed.FS

//go:embed fonts
var fonts embed.FS

//go:embed lib.tgz
var lib []byte

type Config struct {
	// ModelPath    string
	Model        string
	ModelWidth   int
	ModelHeight  int
	EnableCuda   bool
	EnableCoreMl bool
}

type Client struct {
	ModelPath      string
	ModelBasePath  string
	ModelWidth     int
	ModelHeight    int
	LibPath        string
	LibExtractPath string
	RuntimeSession ModelSession
	EnableCuda     bool
	EnableCoreMl   bool
}

type ModelSession struct {
	Session *onnx.AdvancedSession
	Input   *onnx.Tensor[float32]
	Output  *onnx.Tensor[float32]
}

type Object struct {
	ClassName  string
	ClassID    int
	Confidence float32
	X1         float32 // Left
	Y1         float32 // Top
	X2         float32 // Right
	Y2         float32 // Bottom
}

func Init(opt Config) (*Client, error) {
	client := Client{}

	// Determine OS and architecture and set libPath
	hostOs := runtime.GOOS
	hostArch := runtime.GOARCH
	switch {
	case hostOs == "darwin" && hostArch == "arm64":
		if opt.EnableCuda {
			return &Client{}, fmt.Errorf("cuda not supported on darwin/arm64")
		}
		client.LibPath = "lib/osx_arm64/libonnxruntime.dylib"
	case hostOs == "linux" && hostArch == "amd64":
		if opt.EnableCoreMl {
			return &Client{}, fmt.Errorf("coreml not supported on linux/amd64")
		}
		if opt.EnableCuda {
			client.LibPath = "lib/linux_amd64_gpu/libonnxruntime.so"
		} else {
			client.LibPath = "lib/linux_amd64_cpu/libonnxruntime.so"
		}
	case hostOs == "linux" && hostArch == "arm64":
		if opt.EnableCuda || opt.EnableCoreMl {
			return &Client{}, fmt.Errorf("cuda and coreml not supported on linux/arm64")
		}
		client.LibPath = "lib/linux_arm64_cpu/libonnxruntime.so"
	default:
		return &Client{}, fmt.Errorf("unsupported OS or architecture: %s/%s", hostOs, hostArch)
	}

	// Store libPath in temp file
	var err error
	client.LibExtractPath, err = extractLibs(lib, "/tmp")
	if err != nil {
		return &Client{}, err
	}

	// Check if library file exists
	if _, err := os.Stat(client.LibExtractPath + "/" + client.LibPath); err != nil {
		return &Client{}, fmt.Errorf("libPath does not exist: %s", client.LibPath)
	}

	// Prepare models
	modelTempPath, err := extractModels(models, "/tmp")
	if err != nil {
		return &Client{}, err
	}
	client.ModelBasePath = modelTempPath // Set base path for extracted models so it can be cleaned up later

	switch opt.Model {
	case "yolov8n":
		client.ModelPath = modelTempPath + "/models/yolov8n.onnx"
	case "yolov8s":
		client.ModelPath = modelTempPath + "/models/yolov8s.onnx"
	case "yolov8m":
		client.ModelPath = modelTempPath + "/models/yolov8m.onnx"
	default:
		client.ModelPath = modelTempPath + "/models/yolov8n.onnx"
	}

	// Check if model file exists
	if _, err := os.Stat(client.ModelPath); err != nil {
		return &Client{}, fmt.Errorf("modelPath does not exist: %s", client.ModelPath)
	}

	// Set model width/height
	client.ModelWidth = opt.ModelWidth
	client.ModelHeight = opt.ModelHeight

	// If model width/height not provided default to 640x640
	if client.ModelWidth == 0 {
		client.ModelWidth = 640
	}

	if client.ModelHeight == 0 {
		client.ModelHeight = 640
	}

	// Copy other options
	client.EnableCuda = opt.EnableCuda
	client.EnableCoreMl = opt.EnableCoreMl

	// Create session
	ses, err := client.initSession()
	if err != nil {
		return &Client{}, err
	}
	client.RuntimeSession = ses
	return &client, nil
}

func (c *Client) Predict(imgRaw image.Image) ([]Object, *image.RGBA, error) {
	input, img_width, img_height, resizedImage := c.prepareInput(imgRaw)
	inputTensor := c.RuntimeSession.Input.GetData()

	// inTensor := modelSes.Input.GetData()
	copy(inputTensor, input)
	err := c.RuntimeSession.Session.Run()
	if err != nil {
		return nil, nil, fmt.Errorf("error running session: %w", err)
	}

	objects := processOutput(c.RuntimeSession.Output.GetData(), img_width, img_height)
	return objects, resizedImage, nil
}

func (c *Client) initSession() (ModelSession, error) {
	// Change dir to libExtractPath and then change back
	cwd, err := os.Getwd()
	if err != nil {
		return ModelSession{}, err
	}
	err = os.Chdir(c.LibExtractPath) // Change dir to libExtractPath
	if err != nil {
		return ModelSession{}, err
	}

	onnx.SetSharedLibraryPath(c.LibPath) // Set libPath
	err = onnx.InitializeEnvironment()
	if err != nil {
		return ModelSession{}, err
	}
	err = os.Chdir(cwd) // Change back to cwd
	if err != nil {
		return ModelSession{}, err
	}

	options, e := onnx.NewSessionOptions()
	if e != nil {
		return ModelSession{}, fmt.Errorf("error creating session options: %w", e)
	}
	defer options.Destroy()

	if c.EnableCoreMl { // If CoreML is enabled, append the CoreML execution provider
		e = options.AppendExecutionProviderCoreML(0)
		if e != nil {
			options.Destroy()
			return ModelSession{}, err
		}
		defer options.Destroy()
	}

	// Create and prepare a blank image
	blankImage := CreateBlankImage(c.ModelWidth, c.ModelHeight)
	input, _, _, _ := c.prepareInput(blankImage)

	inputShape := onnx.NewShape(1, 3, 640, 640)
	inputTensor, err := onnx.NewTensor(inputShape, input)
	if err != nil {
		return ModelSession{}, fmt.Errorf("error creating input tensor: %w", err)
	}

	outputShape := onnx.NewShape(1, 84, 8400)
	outputTensor, err := onnx.NewEmptyTensor[float32](outputShape)
	if err != nil {
		return ModelSession{}, fmt.Errorf("error creating output tensor: %w", err)
	}

	session, err := onnx.NewAdvancedSession(c.ModelPath,
		[]string{"images"}, []string{"output0"},
		[]onnx.ArbitraryTensor{inputTensor}, []onnx.ArbitraryTensor{outputTensor}, options)

	return ModelSession{
		Session: session,
		Input:   inputTensor,
		Output:  outputTensor,
	}, nil
}

func (c *Client) prepareInput(imageObj image.Image) ([]float32, int64, int64, *image.RGBA) {
	// Get the original image size
	imageSize := imageObj.Bounds().Size()
	imageWidth, imageHeight := int64(imageSize.X), int64(imageSize.Y)

	// Calculate new dimensions to preserve aspect ratio
	ratio := math.Min(float64(c.ModelWidth)/float64(imageWidth), float64(c.ModelHeight)/float64(imageHeight))
	newWidth := int(float64(imageWidth) * ratio)
	newHeight := int(float64(imageHeight) * ratio)

	// Create a new image with the model dimensions and fill it with a black background
	paddedImage := image.NewRGBA(image.Rect(0, 0, c.ModelWidth, c.ModelHeight))
	black := color.RGBA{0, 0, 0, 255}
	draw.Draw(paddedImage, paddedImage.Bounds(), &image.Uniform{black}, image.Point{}, draw.Src)

	// Resize the original image
	resizedImage := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	draw.CatmullRom.Scale(resizedImage, resizedImage.Bounds(), imageObj, imageObj.Bounds(), draw.Over, nil)

	// Calculate where to paste the resized image onto the black background
	dx := (c.ModelWidth - newWidth) / 2
	dy := (c.ModelHeight - newHeight) / 2

	rect := image.Rect(dx, dy, newWidth+dx, newHeight+dy)
	draw.Draw(paddedImage, rect, resizedImage, image.Point{0, 0}, draw.Over)

	// Initialize slice to store the final input, pre-allocate memory
	totalPixels := c.ModelWidth * c.ModelHeight
	inputArray := make([]float32, totalPixels*3)

	// Initialize a WaitGroup to track the completion of goroutines
	var wg sync.WaitGroup

	// Define number of goroutines to use
	numGoroutines := runtime.NumCPU()

	// Calculate rows per goroutine
	rowsPerGoroutine := c.ModelHeight / numGoroutines

	for i := 0; i < numGoroutines; i++ {
		// Calculate the start and end rows for this goroutine
		startY := i * rowsPerGoroutine
		endY := (i + 1) * rowsPerGoroutine
		if i == numGoroutines-1 {
			endY = c.ModelHeight
		}

		wg.Add(1)

		go func(startY, endY int) {
			defer wg.Done()

			var pixelColor color.Color
			var r, g, b uint32

			for y := startY; y < endY; y++ {
				for x := 0; x < c.ModelWidth; x++ {
					pixelColor = paddedImage.At(x, y)
					r, g, b, _ = pixelColor.RGBA()

					redIdx := y*c.ModelWidth + x
					greenIdx := redIdx + c.ModelWidth*c.ModelHeight
					blueIdx := greenIdx + c.ModelWidth*c.ModelHeight

					inputArray[redIdx] = float32(r/257) / 255.0
					inputArray[greenIdx] = float32(g/257) / 255.0
					inputArray[blueIdx] = float32(b/257) / 255.0
				}
			}
		}(startY, endY)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	return inputArray, int64(c.ModelWidth), int64(c.ModelHeight), paddedImage
}

func processOutput(output []float32, imgWidth, imgHeight int64) []Object {
	objects := []Object{}

	for idx := 0; idx < 8400; idx++ {
		classID, probability := 0, float32(0.0)
		for col := 0; col < 80; col++ {
			currentProb := output[8400*(col+4)+idx]
			if currentProb > probability {
				probability = currentProb
				classID = col
			}
		}

		if probability < 0.5 {
			continue
		}

		label := Yolo_classes[classID]

		xc, yc, w, h := output[idx], output[8400+idx], output[2*8400+idx], output[3*8400+idx]
		x1 := (xc - w/2) / 640 * float32(imgWidth)
		y1 := (yc - h/2) / 640 * float32(imgHeight)
		x2 := (xc + w/2) / 640 * float32(imgWidth)
		y2 := (yc + h/2) / 640 * float32(imgHeight)

		objects = append(objects, Object{ClassName: label, ClassID: classID, Confidence: probability, X1: x1, Y1: y1, X2: x2, Y2: y2})
	}

	// Sort the objects by confidence
	sort.Slice(objects, func(i, j int) bool {
		return objects[i].Confidence < objects[j].Confidence
	})

	// Define a slice to hold the final result
	result := []Object{}

	// Iterate through sorted objects, removing overlaps
	for len(objects) > 0 {
		firstObject := objects[0]
		result = append(result, firstObject)
		tmp := []Object{}
		for _, object := range objects[1:] { // Skip the first object
			if iou(firstObject, object) < 0.7 {
				tmp = append(tmp, object)
			}
		}
		objects = tmp
	}

	return result
}

func iou(box1, box2 Object) float64 {
	// Calculate the area of intersection between the two bounding boxes using the intersection function
	intersectArea := intersection(box1, box2)

	// Calculate the union of the two bounding boxes using the union function
	unionArea := union(box1, box2)

	// The Intersection over Union (IoU) is the ratio of the intersection area to the union area
	return intersectArea / unionArea
}

func union(box1, box2 Object) float64 {
	// Extract coordinates of the first rectangle
	rect1Left, rect1Bottom, rect1Right, rect1Top := box1.X1, box1.Y1, box1.X2, box1.Y2

	// Extract coordinates of the second rectangle
	rect2Left, rect2Bottom, rect2Right, rect2Top := box2.X1, box2.Y1, box2.X2, box2.Y2

	// Calculate area of the first rectangle
	rect1Area := (float64(rect1Right) - float64(rect1Left)) * (float64(rect1Top) - float64(rect1Bottom))

	// Calculate area of the second rectangle
	rect2Area := (float64(rect2Right) - float64(rect2Left)) * (float64(rect2Top) - float64(rect2Bottom))

	// Use the intersection function to calculate the area of overlap between the two rectangles
	intersectArea := intersection(box1, box2)

	// The union of two rectangles is the sum of their areas minus the area of their overlap
	return rect1Area + rect2Area - intersectArea
}

func intersection(box1, box2 Object) float64 {
	// Extracting the coordinates of the first box
	firstBoxX1, firstBoxY1, firstBoxX2, firstBoxY2 := box1.X1, box1.Y1, box1.X2, box1.Y2

	// Extracting the coordinates of the second box
	secondBoxX1, secondBoxY1, secondBoxX2, secondBoxY2 := box2.X1, box2.Y1, box2.X2, box2.Y2

	// Calculating the x coordinate of the left side of the intersection
	intersectX1 := math.Max(float64(firstBoxX1), float64(secondBoxX1))

	// Calculating the y coordinate of the bottom side of the intersection
	intersectY1 := math.Max(float64(firstBoxY1), float64(secondBoxY1))

	// Calculating the x coordinate of the right side of the intersection
	intersectX2 := math.Min(float64(firstBoxX2), float64(secondBoxX2))

	// Calculating the y coordinate of the top side of the intersection
	intersectY2 := math.Min(float64(firstBoxY2), float64(secondBoxY2))

	// If there is no intersection, return 0
	if intersectX2 < intersectX1 || intersectY2 < intersectY1 {
		return 0
	}

	// Calculating and returning the area of the intersection
	return (intersectX2 - intersectX1) * (intersectY2 - intersectY1)
}

// Array of YOLOv8 class labels
var Yolo_classes = []string{
	"person", "bicycle", "car", "motorcycle", "airplane", "bus", "train", "truck", "boat",
	"traffic light", "fire hydrant", "stop sign", "parking meter", "bench", "bird", "cat", "dog", "horse",
	"sheep", "cow", "elephant", "bear", "zebra", "giraffe", "backpack", "umbrella", "handbag", "tie",
	"suitcase", "frisbee", "skis", "snowboard", "sports ball", "kite", "baseball bat", "baseball glove",
	"skateboard", "surfboard", "tennis racket", "bottle", "wine glass", "cup", "fork", "knife", "spoon",
	"bowl", "banana", "apple", "sandwich", "orange", "broccoli", "carrot", "hot dog", "pizza", "donut",
	"cake", "chair", "couch", "potted plant", "bed", "dining table", "toilet", "tv", "laptop", "mouse",
	"remote", "keyboard", "cell phone", "microwave", "oven", "toaster", "sink", "refrigerator", "book",
	"clock", "vase", "scissors", "teddy bear", "hair drier", "toothbrush",
}

// Extract compressed tar.gz data to a directory.
func extractTarGz(data []byte, dir string) error {
	gzReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(dir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			file, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(file, tarReader); err != nil {
				return err
			}
			file.Close()
		case tar.TypeSymlink:
			err := os.Symlink(header.Linkname, target)
			if err != nil {
				return err
			}
		case tar.TypeLink:
			err := os.Link(filepath.Join(dir, header.Linkname), target)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func extractLibs(data []byte, tempBasePath string) (string, error) {
	// Remove if exists and recreate a dir named objectPredictLibs.f4fc215193baa83 in tempBasePath
	tempDir := filepath.Join(tempBasePath, "objectPredictLibs.6171c35bdc")
	if err := os.RemoveAll(tempDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(tempDir, os.ModePerm); err != nil {
		return "", err
	}

	// Extract the compressed tar.gz data to the temporary directory
	if err := extractTarGz(data, tempDir); err != nil {
		return "", err
	}

	return tempDir, nil
}

func extractModels(embedFS embed.FS, tempBasePath string) (string, error) {
	// Remove if exists and recreate a dir named objectPredictLibs.f4fc215193baa83 in tempBasePath
	tempDir := filepath.Join(tempBasePath, "objectPredictModels.a30da82de8eae8")
	if err := os.RemoveAll(tempDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(tempDir, os.ModePerm); err != nil {
		return "", err
	}

	// Recursive function to extract files and directories
	var walkDir func(fs.FS, string, string) error
	walkDir = func(fsys fs.FS, path string, currentTempDir string) error {
		dir, err := fs.ReadDir(fsys, path)
		if err != nil {
			return err
		}

		for _, entry := range dir {
			entryName := entry.Name()
			sourcePath := filepath.Join(path, entryName)
			destPath := filepath.Join(currentTempDir, entryName)

			if entry.IsDir() {
				if err := os.MkdirAll(destPath, os.ModePerm); err != nil {
					return err
				}
				if err := walkDir(fsys, sourcePath, destPath); err != nil {
					return err
				}
			} else {
				srcFile, err := fsys.Open(sourcePath)
				if err != nil {
					return err
				}
				defer srcFile.Close()

				destFile, err := os.Create(destPath)
				if err != nil {
					return err
				}
				defer destFile.Close()

				if _, err := io.Copy(destFile, srcFile); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if err := walkDir(embedFS, ".", tempDir); err != nil {
		return "", err
	}

	return tempDir, nil
}

func (c *Client) Close() {
	// Cleanup temp dir
	if c.LibExtractPath != "" {
		if err := os.RemoveAll(c.LibExtractPath); err != nil {
			fmt.Println("Warning: error removing LibExtractPath:", err)
		}
	}

	// Cleanup temp dir for model
	if c.ModelBasePath != "" {
		if err := os.RemoveAll(c.ModelBasePath); err != nil {
			fmt.Println("Warning: error removing ModelBasePath:", err)
		}
	}

	c.RuntimeSession.Session.Destroy() // Cleanup session
	c.RuntimeSession.Input.Destroy()   // Cleanup input
	c.RuntimeSession.Output.Destroy()  // Cleanup output
}

func CreateBlankImage(width, height int) image.Image {
	// Create a new blank image with the given dimensions
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Fill the image with the background color
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{0, 0, 0, 0})
		}
	}

	return img
}

// ***** Helper Functions *****

// DrawRectangle draws a rectangle on an image
func DrawRectangle(img *image.RGBA, rect image.Rectangle, col color.Color, thickness int) {
	for i := 0; i < thickness; i++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			for y := rect.Min.Y + i; y < rect.Min.Y+i+1; y++ {
				img.Set(x, y, col) // Top border
			}
			for y := rect.Max.Y - i - 1; y < rect.Max.Y-i; y++ {
				img.Set(x, y, col) // Bottom border
			}
		}
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X + i; x < rect.Min.X+i+1; x++ {
				img.Set(x, y, col) // Left border
			}
			for x := rect.Max.X - i - 1; x < rect.Max.X-i; x++ {
				img.Set(x, y, col) // Right border
			}
		}
	}
}

// ConvertToRGBA converts an image.Image to *image.RGBA
func ConvertToRGBA(img image.Image) *image.RGBA {
	// Create a new RGBA image with the same dimensions
	rgba := image.NewRGBA(img.Bounds())

	// Draw the original image onto the new RGBA image
	draw.Draw(rgba, rgba.Bounds(), img, image.Point{}, draw.Src)

	return rgba
}

// AddLabelWithTTF adds a label to an image using a TrueType font
func AddLabelWithTTF(img draw.Image, text string, pt image.Point, textColor color.Color, fontSize float64) {
	fontBytes, err := fonts.ReadFile("fonts/Changes.ttf")
	if err != nil {
		panic(err)
	}

	font, err := freetype.ParseFont(fontBytes)
	if err != nil {
		panic(err)
	}
	// Set up the freetype context to draw the text
	c := freetype.NewContext()
	c.SetDPI(72)
	c.SetFont(font)
	c.SetFontSize(fontSize)
	c.SetClip(img.Bounds())
	c.SetDst(img)
	c.SetSrc(image.NewUniform(textColor))

	// Draw the text
	_, err = c.DrawString(text, fixed.Point26_6{
		X: fixed.I(pt.X),
		Y: fixed.I(pt.Y),
	})
	if err != nil {
		log.Printf("Error drawing string: %v", err)
	}
}

// SaveJPEG saves an image to a JPEG file
func SaveJPEG(filename string, img *image.RGBA, quality int) {
	file, err := os.Create(filename)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	options := &jpeg.Options{Quality: quality} // Quality ranges from 1 to 100
	err = jpeg.Encode(file, img, options)
	if err != nil {
		panic(err)
	}
}

// SavePNG saves an image to a PNG file
func SavePNG(filename string, img *image.RGBA) {
	file, err := os.Create(filename)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	err = png.Encode(file, img)
	if err != nil {
		panic(err)
	}
}

// loadImage loads an image from a file. The image type can be either PNG or JPEG.
func LoadImage(filename string) (image.Image, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Automatically detect the file format (JPEG, PNG, etc.)
	img, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}

	return img, nil
}
