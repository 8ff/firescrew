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
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	"github.com/8ff/gonnx"
	"github.com/nfnt/resize"
)

//go:embed models
var models embed.FS

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
	ModelWidth     int
	ModelHeight    int
	LibPath        string
	LibExtractPath string
	Session        *gonnx.SessionV3
	EnableCuda     bool
	EnableCoreMl   bool
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
	client.Session = ses
	return &client, nil
}

func (c *Client) Predict(imgRaw image.Image) ([]Object, error) {
	input, img_width, img_height := c.prepareInput(imgRaw)
	inputShape := gonnx.NewShape(1, 3, 640, 640)
	inputTensor, err := gonnx.NewTensor(inputShape, input)
	if err != nil {
		return nil, err
	}

	output, e := c.Session.Run([]*gonnx.TensorWithType{{
		Tensor:     inputTensor,
		TensorType: "float32",
	}})
	if e != nil {
		return nil, fmt.Errorf("error running session: %w", e)
	}

	var allFloat32Data []float32

	for i := range output {
		data := output[i].GetData()
		float32Data, ok := data.([]float32)
		if !ok {
			continue
		}
		allFloat32Data = append(allFloat32Data, float32Data...)
	}

	objects := processOutput(allFloat32Data, img_width, img_height)

	return objects, nil
}

func (c *Client) initSession() (*gonnx.SessionV3, error) {
	// Change dir to libExtractPath and then change back
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	err = os.Chdir(c.LibExtractPath) // Change dir to libExtractPath
	if err != nil {
		return nil, err
	}

	gonnx.SetSharedLibraryPath(c.LibPath) // Set libPath
	err = gonnx.InitializeEnvironment()
	if err != nil {
		return nil, err
	}
	err = os.Chdir(cwd) // Change back to cwd
	if err != nil {
		return nil, err
	}

	var opts string
	switch {
	case c.EnableCuda:
		opts = "cuda"
	case c.EnableCoreMl:
		opts = "coreml"
	default:
		opts = ""
	}

	session, e := gonnx.NewSessionV3(c.ModelPath, opts)
	if e != nil {
		return nil, fmt.Errorf("error creating session: %w", e)
	}
	return session, nil
}

func (c *Client) prepareInput(imageObj image.Image) ([]float32, int64, int64) {
	// Get the original image size
	imageSize := imageObj.Bounds().Size()
	imageWidth, imageHeight := int64(imageSize.X), int64(imageSize.Y)

	// Resize the image to modelWidth*modelHeight only if it is not already of that size
	var resizedImage image.Image
	if imageWidth != int64(c.ModelWidth) || imageHeight != int64(c.ModelHeight) {
		resizedImage = resize.Resize(uint(c.ModelWidth), uint(c.ModelHeight), imageObj, resize.Lanczos3)
	} else {
		resizedImage = imageObj
	}

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
					pixelColor = resizedImage.At(x, y)
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

	return inputArray, imageWidth, imageHeight
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
	// Create a temporary directory within the base path
	tempDir, err := os.MkdirTemp(tempBasePath, "")
	if err != nil {
		return "", err
	}

	// Extract the compressed tar.gz data to the temporary directory
	if err := extractTarGz(data, tempDir); err != nil {
		return "", err
	}

	return tempDir, nil
}

func extractModels(embedFS embed.FS, tempBasePath string) (string, error) {
	// Create a temporary directory within the base path
	tempDir, err := os.MkdirTemp(tempBasePath, "")
	if err != nil {
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
	if c.ModelPath != "" {
		modelDir := filepath.Dir(c.ModelPath)
		if err := os.RemoveAll(modelDir); err != nil {
			fmt.Println("Warning: error removing ModelPath directory:", err)
		}
	}

	c.Session.Destroy() // Cleanup session
}
