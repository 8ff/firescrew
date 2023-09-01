package main

import (
	"fmt"
	"image"
	"image/color"
	"time"

	"github.com/8ff/firescrew/pkg/objectPredict"
	"github.com/8ff/prettyTimer"
)

func main() {
	filename := "a.jpg" // Replace this with the path to your PNG file
	img, err := objectPredict.LoadImage(filename)
	if err != nil {
		fmt.Println("Error loading image:", err)
		return
	}

	obj, err := objectPredict.Init(objectPredict.Config{Model: "yolov8s", EnableCoreMl: true})
	if err != nil {
		fmt.Println("Cannot init model:", err)
		return
	}

	defer obj.Close() // Cleanup files
	stats := prettyTimer.NewTimingStats()

	start := time.Now()

	objects, resizedImage, err := obj.Predict(img)
	if err != nil {
		fmt.Println("Cannot predict:", err)
		return
	}
	// End timer
	stats.RecordTiming(time.Since(start))

	// Convert to RGBA once, and use the same frame for all objects
	frame := objectPredict.ConvertToRGBA(resizedImage)

	for _, object := range objects {
		fmt.Println("Object:", object)
		rect := image.Rect(int(object.X1), int(object.Y1), int(object.X2), int(object.Y2))
		objectPredict.DrawRectangle(frame, rect, color.RGBA{255, 165, 0, 255}, 2) // Draw orange rectangle
		pt := image.Pt(int(object.X1), int(object.Y1)-5)
		if int(object.Y1)-5 < 0 {
			pt = image.Pt(int(object.X1), int(object.Y1)+20) // if the box is too close to the top of the image, put the label inside the box
		}
		objectPredict.AddLabelWithTTF(frame, fmt.Sprintf("%s %.2f", object.ClassName, object.Confidence), pt, color.RGBA{255, 165, 0, 255}, 12.0) // Orange size 12 font
	}

	// Save the image once, after drawing all rectangles and labels
	objectPredict.SaveJPEG("out.jpg", frame, 100)

	stats.PrintStats()
}
