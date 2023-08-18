#!/usr/bin/env python3.10
# This runs on python 3.10
# pip3.10 install coremltools Pillow

import coremltools as ct
import numpy as np
import PIL.Image
import socket
import threading
import io
import json
import time
import traceback


# Load the Core ML model
# model_path = "./YOLOv3.mlmodel"
model_path = "./yolov8s.mlmodel"
model = ct.models.MLModel(model_path)
# print model details
# print(model)

class_labels = {
    0: 'person',
    1: 'bicycle',
    2: 'car',
    3: 'motorcycle',
    4: 'airplane',
    5: 'bus',
    6: 'train',
    7: 'truck',
    8: 'boat',
    9: 'traffic light',
    10: 'fire hydrant',
    12: 'stop sign',
    13: 'parking meter',
    14: 'bench',
    15: 'bird',
    16: 'cat',
    17: 'dog',
    18: 'horse',
    19: 'sheep',
    20: 'cow',
    21: 'elephant',
    22: 'bear',
    23: 'zebra',
    24: 'giraffe',
    26: 'backpack',
    27: 'umbrella',
    30: 'handbag',
    31: 'tie',
    32: 'suitcase',
    33: 'frisbee',
    34: 'skis',
    35: 'snowboard',
    36: 'sports ball',
    37: 'kite',
    38: 'baseball bat',
    39: 'baseball glove',
    40: 'skateboard',
    41: 'surfboard',
    42: 'tennis racket',
    43: 'bottle',
    45: 'wine glass',
    46: 'cup',
    47: 'fork',
    48: 'knife',
    49: 'spoon',
    50: 'bowl',
    51: 'banana',
    52: 'apple',
    53: 'sandwich',
    54: 'orange',
    55: 'broccoli',
    56: 'carrot',
    57: 'hot dog',
    58: 'pizza',
    59: 'donut',
    60: 'cake',
    61: 'chair',
    62: 'couch',
    63: 'potted plant',
    64: 'bed',
    66: 'dining table',
    69: 'toilet',
    71: 'tv',
    72: 'laptop',
    73: 'mouse',
    74: 'remote',
    75: 'keyboard',
    76: 'cell phone',
    77: 'microwave',
    78: 'oven',
    79: 'toaster',
    80: 'sink',
    81: 'refrigerator',
    83: 'book',
    84: 'clock',
    85: 'vase',
    86: 'scissors',
    87: 'teddy bear',
    88: 'hair drier',
    89: 'toothbrush',
}

def load_image(data, target_size=(640, 640)):
    img = PIL.Image.open(io.BytesIO(data))
    img_aspect = img.width / img.height
    target_aspect = target_size[0] / target_size[1]

    # Resize the image to maintain aspect ratio
    if img_aspect > target_aspect:
        new_width = target_size[0]
        new_height = int(new_width / img_aspect)
    else:
        new_height = target_size[1]
        new_width = int(new_height * img_aspect)

    img_resized = img.resize((new_width, new_height))

    # Create a new blank image with the target size
    final_img = PIL.Image.new('RGB', target_size)

    # Compute the padding required in each dimension
    x_padding = (target_size[0] - new_width) // 2
    y_padding = (target_size[1] - new_height) // 2

    # Paste the resized image into the blank image, centered
    final_img.paste(img_resized, (x_padding, y_padding))

    return final_img  # Return the PIL Image object


def recvall(sock, count):
    buf = b''
    while count:
        newbuf = sock.recv(count)
        if not newbuf: return None
        buf += newbuf
        count -= len(newbuf)
    return buf

def handle_client(conn):
    try:
        while True:
            frame_len_bytes = conn.recv(4)
            if not frame_len_bytes:
                break
            frame_len = int.from_bytes(frame_len_bytes, 'big')
            frame_data = recvall(conn, frame_len)
            if frame_data is None:
                break

            # Load and resize the image (now returning a PIL Image object)
            img_resized = load_image(frame_data)

            # Run the model
            out_dict = model.predict({'image': img_resized})  # Pass the PIL Image object

           # Check if the 'confidence' array has elements
            if out_dict['confidence'].shape[0] == 0:
                print("No objects detected")
                continue  # Skip to the next iteration if no objects are detected

            # Extract the results
            predictions = []
            for i, confidence in enumerate(out_dict['confidence'][0]):
                if confidence > 0 and i in class_labels:
                    coordinates = out_dict['coordinates'][0]
                    x_center, y_center, width, height = coordinates
                    x_center *= 640  # Scale to the model's expected size
                    y_center = (y_center * 640 - 140) # Adjust y_center to match the original image size
                    width *= 640 # Scale to the model's expected size
                    height = (height * 640) / 2 # Adjust height to match the original image size
                    left = int(x_center - (width / 2))
                    top = int(y_center - (height / 2))
                    right = int(x_center + (width / 2))
                    bottom = int(y_center + (height / 2))
                    prediction = {
                        'object': i + 1,
                        'class_name': class_labels[i],
                        'box': [left, top, right, bottom],
                        'confidence': float(confidence),
                    }
                    predictions.append(prediction)

            # Convert the predictions to a JSON string
            predictions_json = json.dumps(predictions)

            # Send the results back to the client
            conn.sendall((predictions_json + '\n').encode())
    except Exception as e:
        print(f"Exception handling client: {e}")
        traceback.print_exc()  # Print the full traceback
    finally:
        print("Closing connection")
        conn.close()


def main():
    LISTEN_ADDR = "0.0.0.0"
    LISTEN_PORT = 8555

    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    s.bind((LISTEN_ADDR, LISTEN_PORT))
    s.listen(5)

    print(f"Server is listening on {LISTEN_ADDR}:{LISTEN_PORT}")

    while True:
        conn, addr = s.accept()
        print(f"Got connection from {addr}")
        thread = threading.Thread(target=handle_client, args=(conn,))
        thread.start()

if __name__ == "__main__":
    main()
