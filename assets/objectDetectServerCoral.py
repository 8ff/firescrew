#!/usr/bin/env python3
from PIL import Image
from pycoral.adapters import common
from pycoral.adapters import detect
from pycoral.utils.edgetpu import make_interpreter
import numpy as np
import socket
import json
import threading
import io

# Load the TFLite model
model_path = "mobilenet_ssd_v2_coco_quant_postprocess_edgetpu.tflite"
interpreter = make_interpreter(model_path)
interpreter.allocate_tensors()

# Class labels dictionary (same as provided)
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
            # Read the frame length (assumed to be sent as a 4-byte integer)
            frame_len_bytes = conn.recv(4)
            if not frame_len_bytes:
                break
            frame_len = int.from_bytes(frame_len_bytes, 'big')

            # Read the frame data
            frame_data = recvall(conn, frame_len)
            if frame_data is None:
                break

            # Convert the raw bytes into an image
            image = Image.open(io.BytesIO(frame_data))

            # Capture the original dimensions
            original_width, original_height = image.size

            # Resize and preprocess the image
            resized_width = 300
            resized_height = 300
            image_resized = image.resize((resized_width, resized_height))
            input_data = np.asarray(image_resized)
            common.set_input(interpreter, input_data)

            # Run the model
            interpreter.invoke()

            # Extract the output and postprocess it
            results = detect.get_objects(interpreter, score_threshold=0.5)

            # Scaling factors
            scale_x = original_width / resized_width
            scale_y = original_height / resized_height

            predictions = []
            for result in results:
                if result.id in class_labels:
                # Assuming result.bbox is in the format [left, top, right, bottom]
                    left, top, right, bottom = result.bbox
                prediction = {
            'object': result.id + 1,
            'class_name': class_labels[result.id],
            'box': [int(left * scale_x), int(top * scale_y), int(right * scale_x), int(bottom * scale_y)], # Adjusted format
            'confidence': float(result.score)
                }
            predictions.append(prediction)



            # Convert the predictions to a JSON string
            predictions_json = json.dumps(predictions)

            # Send the results back to the client
            conn.sendall((predictions_json + '\n').encode())
    except Exception as e:
        print(f"Exception handling client: {e}")
    finally:
        # Close the connection when the loop exits
        print("Closing connection")
        conn.close()


def main():
    LISTEN_ADDR = "0.0.0.0"
    LISTEN_PORT = 8555

    # Create a socket object
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)

    # Set the SO_REUSEADDR flag
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)

    # Bind the socket to a public host, and a port
    s.bind((LISTEN_ADDR, LISTEN_PORT))
    s.listen(5)

    print("Server is listening on %s:%d" % (LISTEN_ADDR, LISTEN_PORT))

    while True:
        # Establish a connection with the client
        conn, addr = s.accept()
        print(f"Got connection from {addr}")

        # Handle the client connection in a new thread
        thread = threading.Thread(target=handle_client, args=(conn,))
        thread.start()

if __name__ == "__main__":
    main()